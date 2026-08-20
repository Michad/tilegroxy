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

//go:build e2e

package e2e

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// How long a single request under load may take before it counts as a failure. Shorter than
// requestTimeout so a stalled request surfaces inside the window a continuity test observes.
const loadRequestTimeout = 15 * time.Second

// LoadResult tallies what a load run observed. The distinction that matters for shutdown tests is
// between a connection refused at dial time (acceptable once the listener closes) and a connection
// that was accepted and then broken (never acceptable).
type LoadResult struct {
	Total           int
	ByStatus        map[int]int
	TransportErrors int
	RefusedErrors   int
	MaxLatency      time.Duration
}

// AllOK reports the property most continuity tests assert: every request returned 200 and nothing
// failed at the transport level.
func (r LoadResult) AllOK() bool {
	return r.TransportErrors == 0 && r.Total > 0 && r.ByStatus[http.StatusOK] == r.Total
}

// Load is a running set of workers hammering one URL until Stop.
type Load struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	result LoadResult
}

// StartLoad drives sustained traffic until Stop. Continuity assertions reduce to running this
// across a disruption and checking nothing failed.
func (i *Instance) StartLoad(path string, workers int) *Load {
	ctx, cancel := context.WithCancel(context.Background())

	l := &Load{cancel: cancel, result: LoadResult{ByStatus: map[int]int{}}}

	url := i.BaseURL() + path

	for range workers {
		l.wg.Add(1)

		go func() {
			defer l.wg.Done()

			client := &http.Client{Timeout: Scale(loadRequestTimeout)}

			for ctx.Err() == nil {
				l.once(ctx, client, url)
			}
		}()
	}

	return l
}

func (l *Load) once(ctx context.Context, client *http.Client, url string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if ctx.Err() != nil {
		// Cancellation during Stop is the harness shutting down, not a server failure, so the
		// request is not counted at all.
		return
	}

	l.result.Total++

	if elapsed > l.result.MaxLatency {
		l.result.MaxLatency = elapsed
	}

	if err != nil {
		l.result.TransportErrors++

		if isRefused(err) {
			l.result.RefusedErrors++
		}

		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	l.result.ByStatus[resp.StatusCode]++
}

// isRefused separates "the listener is gone" from "the connection broke mid-flight". Matching on
// message text is crude but avoids a syscall-package dependency that differs across platforms.
func isRefused(err error) bool {
	msg := err.Error()

	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host")
}

// Stop cancels the workers and returns what they observed. In-flight requests are cancelled rather
// than awaited, so Stop returns promptly even against a hung server.
func (l *Load) Stop() LoadResult {
	l.cancel()
	l.wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()

	return l.result
}
