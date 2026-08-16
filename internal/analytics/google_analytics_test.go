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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gaCapture stands in for the Measurement Protocol endpoint.
type gaCapture struct {
	mutex    sync.Mutex
	payloads []gaPayload
	queries  []string
	status   int
}

func (c *gaCapture) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var p gaPayload
		_ = json.Unmarshal(body, &p)

		c.mutex.Lock()
		c.payloads = append(c.payloads, p)
		c.queries = append(c.queries, r.URL.RawQuery)
		status := c.status
		c.mutex.Unlock()

		if status == 0 {
			status = http.StatusNoContent
		}

		w.WriteHeader(status)
	}
}

func (c *gaCapture) snapshot() []gaPayload {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	out := make([]gaPayload, len(c.payloads))
	copy(out, c.payloads)

	return out
}

func newGA(t *testing.T, endpoint string, mutate func(*GoogleAnalyticsConfig)) analytics.Analytics {
	t.Helper()

	cfg := GoogleAnalyticsConfig{
		MeasurementID: "G-TEST",
		APISecret:     "secret-value",
		Endpoint:      endpoint,
	}
	cfg.Batch.MaxSize = 1
	cfg.Batch.MaxAge = 1

	if mutate != nil {
		mutate(&cfg)
	}

	a, err := GoogleAnalyticsRegistration{}.Initialize(cfg, analytics.AnalyticsDeps{ErrorMessages: config.DefaultConfig().Error.Messages})
	require.NoError(t, err)

	return a
}

func closeGA(t *testing.T, a analytics.Analytics) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, a.(*GoogleAnalytics).Close(ctx))
}

func Test_GoogleAnalytics_SendsPayload(t *testing.T) {
	capture := &gaCapture{}
	srv := httptest.NewServer(capture.handler())
	defer srv.Close()

	a := newGA(t, srv.URL, nil)

	require.NoError(t, a.Record(pkg.BackgroundContext(), analytics.Event{
		LayerID: "main",
		Z:       1,
		X:       2,
		Y:       3,
		UserID:  "user-7",
		Fields:  map[string]any{"contenttype": "image/png"},
	}))

	closeGA(t, a)

	payloads := capture.snapshot()
	require.Len(t, payloads, 1)
	require.Len(t, payloads[0].Events, 1)

	event := payloads[0].Events[0]
	assert.Equal(t, gaDefaultEventName, event.Name)
	assert.Equal(t, "main", event.Params["layer"])
	assert.EqualValues(t, 1, event.Params["z"])
	assert.EqualValues(t, 2, event.Params["x"])
	assert.EqualValues(t, 3, event.Params["y"])
	assert.Equal(t, "image/png", event.Params["contenttype"], "configured extra fields should reach GA as params")
	assert.Equal(t, "user-7", payloads[0].ClientID)
	assert.Equal(t, "user-7", payloads[0].UserID)

	capture.mutex.Lock()
	query := capture.queries[0]
	capture.mutex.Unlock()

	assert.Contains(t, query, "measurement_id=G-TEST")
	assert.Contains(t, query, "api_secret=secret-value")
}

func Test_GoogleAnalytics_AnonymousUsesHashedIP(t *testing.T) {
	capture := &gaCapture{}
	srv := httptest.NewServer(capture.handler())
	defer srv.Close()

	a := newGA(t, srv.URL, nil)

	require.NoError(t, a.Record(pkg.BackgroundContext(), analytics.Event{
		LayerID: "main",
		Fields:  map[string]any{analytics.FieldIP: "10.1.2.3"},
	}))

	closeGA(t, a)

	payloads := capture.snapshot()
	require.Len(t, payloads, 1)

	assert.NotEmpty(t, payloads[0].ClientID, "GA requires a client_id on every payload")
	assert.NotContains(t, payloads[0].ClientID, "10.1.2.3", "the raw IP must never be sent to Google")
	assert.Empty(t, payloads[0].UserID, "an anonymous request should not claim a user_id")
}

func Test_GoogleAnalytics_AnonymousWithoutIP(t *testing.T) {
	capture := &gaCapture{}
	srv := httptest.NewServer(capture.handler())
	defer srv.Close()

	a := newGA(t, srv.URL, nil)

	require.NoError(t, a.Record(pkg.BackgroundContext(), analytics.Event{LayerID: "main"}))
	closeGA(t, a)

	payloads := capture.snapshot()
	require.Len(t, payloads, 1)
	assert.Equal(t, "anonymous", payloads[0].ClientID)
}

func Test_GoogleAnalytics_ClampsBatchSize(t *testing.T) {
	capture := &gaCapture{}
	srv := httptest.NewServer(capture.handler())
	defer srv.Close()

	a := newGA(t, srv.URL, func(cfg *GoogleAnalyticsConfig) {
		// Well above the Measurement Protocol's 25 event ceiling.
		cfg.Batch.MaxSize = 500
		cfg.Batch.MaxAge = 600
	})

	ctx := pkg.BackgroundContext()

	for range 30 {
		require.NoError(t, a.Record(ctx, analytics.Event{LayerID: "main", UserID: "u"}))
	}

	closeGA(t, a)

	for _, p := range capture.snapshot() {
		assert.LessOrEqual(t, len(p.Events), gaMaxEventsPerRequest, "a payload must never exceed the GA per-request limit")
	}
}

func Test_GoogleAnalytics_GroupsByClient(t *testing.T) {
	capture := &gaCapture{}
	srv := httptest.NewServer(capture.handler())
	defer srv.Close()

	a := newGA(t, srv.URL, func(cfg *GoogleAnalyticsConfig) {
		cfg.Batch.MaxSize = 100
		cfg.Batch.MaxAge = 600
	})

	ctx := pkg.BackgroundContext()
	require.NoError(t, a.Record(ctx, analytics.Event{LayerID: "main", UserID: "alice"}))
	require.NoError(t, a.Record(ctx, analytics.Event{LayerID: "main", UserID: "bob"}))

	closeGA(t, a)

	payloads := capture.snapshot()
	// GA requires one client_id per payload, so two users cannot share a request.
	require.Len(t, payloads, 2)

	seen := map[string]bool{}
	for _, p := range payloads {
		seen[p.ClientID] = true
	}

	assert.True(t, seen["alice"])
	assert.True(t, seen["bob"])
}

func Test_GoogleAnalytics_ServerErrorIsContained(t *testing.T) {
	capture := &gaCapture{status: http.StatusInternalServerError}
	srv := httptest.NewServer(capture.handler())
	defer srv.Close()

	a := newGA(t, srv.URL, nil)

	// A 500 from GA must not propagate; the batcher logs and counts it.
	require.NoError(t, a.Record(pkg.BackgroundContext(), analytics.Event{LayerID: "main"}))
	closeGA(t, a)

	assert.Len(t, capture.snapshot(), 1)
}

func Test_GoogleAnalytics_RequiredParams(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	_, err := GoogleAnalyticsRegistration{}.Initialize(GoogleAnalyticsConfig{APISecret: "x"}, analytics.AnalyticsDeps{ErrorMessages: msgs})
	require.Error(t, err, "measurementId is required")

	_, err = GoogleAnalyticsRegistration{}.Initialize(GoogleAnalyticsConfig{MeasurementID: "G-X"}, analytics.AnalyticsDeps{ErrorMessages: msgs})
	require.Error(t, err, "apiSecret is required")
}

func Test_ChunkEvents(t *testing.T) {
	events := make([]analytics.Event, 7)

	chunks := chunkEvents(events, 3)
	require.Len(t, chunks, 3)
	assert.Len(t, chunks[0], 3)
	assert.Len(t, chunks[1], 3)
	assert.Len(t, chunks[2], 1)

	assert.Empty(t, chunkEvents(nil, 3))
}
