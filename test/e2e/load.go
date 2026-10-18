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
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"
)

// Shorter than requestTimeout so a stalled request surfaces within a continuity test's window
const loadRequestTimeout = 15 * time.Second

// Separates refusals at dial time, fine once the listener closes, from accepted connections that broke, which never are
type LoadResult struct {
	Total           int
	ByStatus        map[int]int
	TransportErrors int
	RefusedErrors   int
	MaxLatency      time.Duration
}

// Every request returned 200 with no transport failures
func (r LoadResult) AllOK() bool {
	return r.TransportErrors == 0 && r.Total > 0 && r.ByStatus[http.StatusOK] == r.Total
}

// Workers hammering one URL until Stop
type Load struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	result LoadResult
}

// Continuity assertions run this across a disruption and check nothing failed
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
	var reused bool

	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, url, nil)
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
		// Cancellation during Stop is the harness shutting down, so it isn't counted
		return
	}

	l.result.Total++

	if elapsed > l.result.MaxLatency {
		l.result.MaxLatency = elapsed
	}

	if err != nil {
		l.result.TransportErrors++

		if isRefused(err) || (!reused && isBacklogReset(err)) {
			l.result.RefusedErrors++
		}

		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	l.result.ByStatus[resp.StatusCode]++
}

// Message matching is crude but avoids platform-specific syscall dependencies
func isRefused(err error) bool {
	msg := err.Error()

	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host")
}

// Closing a listener resets completed but unaccepted connections, which in practice is a refusal
func isBacklogReset(err error) bool {
	msg := err.Error()

	return errors.Is(err, io.EOF) || strings.Contains(msg, "connection reset by peer") || strings.Contains(msg, "broken pipe")
}

// In-flight requests are cancelled rather than awaited so Stop returns promptly against a hung server
func (l *Load) Stop() LoadResult {
	l.cancel()
	l.wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()

	return l.result
}
