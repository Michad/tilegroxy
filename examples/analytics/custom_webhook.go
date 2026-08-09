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

//go:build ignore

// This example implements a custom analytics module that POSTs each batch of events to a webhook as JSON. Configure it with something like:
//
//	analytics:
//	  - name: custom
//	    file: examples/analytics/custom_webhook.go
//	    url: https://example.com/usage
//	    token: env.WEBHOOK_TOKEN
//	    batch:
//	      maxSize: 100
//	      maxAge: 60

// Package must always be custom
package custom

import (
	//The standard library is available for use
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	//This contains types and utility functions from the main tilegroxy application. It is required to always be imported
	"tilegroxy/tilegroxy"
)

// The shape sent to the webhook. This is kept independent of tilegroxy's own event struct so the payload stays stable if that changes
type usageRecord struct {
	Time    time.Time      `json:"time"`
	Layer   string         `json:"layer"`
	Z       int            `json:"z"`
	X       int            `json:"x"`
	Y       int            `json:"y"`
	User    string         `json:"user,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// This method is responsible for delivering events to their destination. It is called once per batch of events, not once per request
func record(
	//Contextual information about the batch being written
	ctx tilegroxy.Context,
	//The events to record. Each contains Time, LayerID, LayerName, Z, X, Y, UserID and a Fields map
	events []tilegroxy.AnalyticsEvent,
	//The parameters included under the analytics module in the configuration. In this case it will contain "url" and "token"
	params map[string]interface{},
	//A mapping for localization of error messages
	_ tilegroxy.ErrorMessages,
) error {
	url, ok := params["url"].(string)
	if !ok || url == "" {
		return errors.New("the url parameter is required")
	}

	records := make([]usageRecord, 0, len(events))

	for _, e := range events {
		records = append(records, usageRecord{
			Time:    e.Time,
			Layer:   e.LayerID,
			Z:       e.Z,
			X:       e.X,
			Y:       e.Y,
			User:    e.UserID,
			Details: e.Fields,
		})
	}

	body, err := json.Marshal(records)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	//Any extra configuration parameter is available here, so credentials can come from env. or a secret store instead of being written into the script
	if token, ok := params["token"].(string); ok && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	//Returning an error here causes the batch to be logged as failed and dropped. It never affects the tile responses that produced these events
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("webhook returned status " + strconv.Itoa(resp.StatusCode))
	}

	return nil
}
