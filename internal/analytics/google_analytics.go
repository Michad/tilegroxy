// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package analytics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
)

const (
	gaDefaultEndpoint  = "https://www.google-analytics.com/mp/collect"
	gaDefaultEventName = "tile_request"
	// The Measurement Protocol rejects payloads with more than 25 events so batches are capped here
	gaMaxEventsPerRequest = 25
	gaDefaultTimeout      = 10
)

type GoogleAnalyticsConfig struct {
	analytics.CommonConfig `mapstructure:",squash"`
	// The GA4 measurement ID, in the form G-XXXXXXX
	MeasurementID string
	// The Measurement Protocol API secret. Expected to be supplied via env. or secret.
	APISecret string
	// The event name recorded in GA
	EventName string
	// Overrides the collection endpoint. Point this at /debug/mp/collect to validate payloads
	Endpoint string
	// How long (in seconds) a request to GA can be in flight
	Timeout uint
	// The user agent sent to GA
	UserAgent string
}

type GoogleAnalytics struct {
	GoogleAnalyticsConfig
	url     string
	client  *http.Client
	batcher *analytics.Batcher
}

func init() {
	analytics.RegisterAnalytics(GoogleAnalyticsRegistration{})
}

type GoogleAnalyticsRegistration struct {
}

func (s GoogleAnalyticsRegistration) InitializeConfig() any {
	return GoogleAnalyticsConfig{}
}

func (s GoogleAnalyticsRegistration) Name() string {
	return "googleanalytics"
}

func (s GoogleAnalyticsRegistration) Initialize(cfgAny any, _ *datastore.DatastoreRegistry, errorMessages config.ErrorMessages) (analytics.Analytics, error) {
	cfg := cfgAny.(GoogleAnalyticsConfig)

	if cfg.MeasurementID == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "analytics.googleanalytics.measurementid")
	}

	if cfg.APISecret == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "analytics.googleanalytics.apisecret")
	}

	if cfg.EventName == "" {
		cfg.EventName = gaDefaultEventName
	}

	if cfg.Endpoint == "" {
		cfg.Endpoint = gaDefaultEndpoint
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = gaDefaultTimeout
	}

	batchCfg, err := analytics.ApplyBatchDefaults(cfg.Batch, errorMessages)
	if err != nil {
		return nil, err
	}

	id := cfg.ID
	if id == "" {
		id = s.Name()
	}

	if batchCfg.MaxSize > gaMaxEventsPerRequest {
		slog.Warn(fmt.Sprintf("Analytics module %v: batch.maxSize of %v exceeds the Google Analytics limit of %v events per request; using %v instead", id, batchCfg.MaxSize, gaMaxEventsPerRequest, gaMaxEventsPerRequest))
		batchCfg.MaxSize = gaMaxEventsPerRequest
	}

	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "analytics.googleanalytics.endpoint", cfg.Endpoint)
	}

	query := endpoint.Query()
	query.Set("measurement_id", cfg.MeasurementID)
	query.Set("api_secret", cfg.APISecret)
	endpoint.RawQuery = query.Encode()

	g := &GoogleAnalytics{
		GoogleAnalyticsConfig: cfg,
		url:                   endpoint.String(),
		client:                &http.Client{Timeout: time.Duration(cfg.Timeout) * time.Second},
	}

	batcher, err := analytics.NewBatcher(id, batchCfg, g.flush)
	if err != nil {
		return nil, err
	}

	g.batcher = batcher

	return g, nil
}

func (g *GoogleAnalytics) Record(ctx context.Context, event analytics.Event) error {
	return g.batcher.Add(ctx, event)
}

// gaPayload is one Measurement Protocol request. GA requires all events in a request to share a client_id
// so events are grouped by client before being sent
type gaPayload struct {
	ClientID string    `json:"client_id"`
	UserID   string    `json:"user_id,omitempty"`
	Events   []gaEvent `json:"events"`
}

type gaEvent struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params"`
}

func (g *GoogleAnalytics) flush(ctx context.Context, events []analytics.Event) error {
	byClient := make(map[string][]analytics.Event)

	for _, e := range events {
		byClient[g.clientID(e)] = append(byClient[g.clientID(e)], e)
	}

	var errs []error

	for clientID, clientEvents := range byClient {
		for _, chunk := range chunkEvents(clientEvents, gaMaxEventsPerRequest) {
			if err := g.send(ctx, clientID, chunk); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}

	return nil
}

// chunkEvents splits events into groups no larger than size
func chunkEvents(events []analytics.Event, size int) [][]analytics.Event {
	chunks := make([][]analytics.Event, 0, (len(events)+size-1)/size)

	for i := 0; i < len(events); i += size {
		end := min(i+size, len(events))
		chunks = append(chunks, events[i:end])
	}

	return chunks
}

// clientID derives the GA client identifier. GA requires one on every payload so anonymous requests fall
// back to a hash of the source IP, which also means the raw address is never sent to Google
func (g *GoogleAnalytics) clientID(e analytics.Event) string {
	if e.UserID != "" {
		return e.UserID
	}

	if ip, ok := e.Fields[analytics.FieldIP].(string); ok && ip != "" {
		sum := sha256.Sum256([]byte(ip))
		return hex.EncodeToString(sum[:])
	}

	return "anonymous"
}

func (g *GoogleAnalytics) send(ctx context.Context, clientID string, events []analytics.Event) error {
	payload := gaPayload{ClientID: clientID, Events: make([]gaEvent, 0, len(events))}

	for _, e := range events {
		params := map[string]any{
			"layer": e.LayerID,
			"z":     e.Z,
			"x":     e.X,
			"y":     e.Y,
		}

		for k, v := range e.Fields {
			params[k] = v
		}

		if e.UserID != "" {
			payload.UserID = e.UserID
		}

		payload.Events = append(payload.Events, gaEvent{Name: g.EventName, Params: params})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	if g.UserAgent != "" {
		req.Header.Set("User-Agent", g.UserAgent)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close() //nolint:errcheck // Nothing actionable if closing the body fails

	// GA returns 204 on success and 2xx generally, anything else is reported so the batcher counts it as
	// an error. The error never reaches the user
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return errors.New("google analytics returned status " + strconv.Itoa(resp.StatusCode))
	}

	return nil
}

func (g *GoogleAnalytics) Close(ctx context.Context) error {
	return g.batcher.Close(ctx)
}
