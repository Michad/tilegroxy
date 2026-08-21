// Copyright 2024 Michael Davis
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

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"reflect"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/Michad/tilegroxy/internal/checks"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/health"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/Michad/tilegroxy/pkg/static"
)

var startupWaitTime = 100 * time.Millisecond
var checkLeeway = 5 * time.Second

type CheckResult struct {
	err       error
	timestamp time.Time
	ttl       time.Duration
}

type healthHandler struct {
	checks           []health.HealthCheck
	checkResultCache *sync.Map
	// Set when shutdown begins so readiness fails before the server starts draining
	draining *atomic.Bool
}

// checkDetail builds the per-check entry of the health response, reporting whether that check is
// currently passing. A result that's missing or older than its TTL counts as a failure
func checkDetail(i int, check health.HealthCheck, cache *sync.Map) (map[string]any, bool) {
	detail := make(map[string]any)
	detail["componentId"] = strconv.Itoa(i)

	if t := reflect.TypeOf(check); t.Kind() == reflect.Pointer {
		detail["componentType"] = t.Elem().Name()
	} else {
		detail["componentType"] = t.Name()
	}

	result, found := cache.Load(i)
	resultCheck, isResult := result.(CheckResult)

	if !found || !isResult {
		detail["status"] = "error"
		detail["output"] = "Check has not updated stored health value"

		return detail, false
	}

	detail["time"] = resultCheck.timestamp.Format(time.RFC3339)
	detail["ttl"] = resultCheck.ttl / time.Second

	switch {
	case resultCheck.err != nil:
		detail["status"] = "error"
		detail["output"] = resultCheck.err.Error()
	// Include 5 second leeway for check not being performed instantly
	case resultCheck.timestamp.Add(resultCheck.ttl).Add(checkLeeway).Before(time.Now()):
		detail["status"] = "error"
		detail["output"] = "stale"
	default:
		detail["status"] = "ok"

		return detail, true
	}

	return detail, false
}

func (h healthHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	slog.DebugContext(ctx, "server: health handler started")
	defer slog.DebugContext(ctx, "server: health handler ended")

	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if h.draining != nil && h.draining.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)

		if _, err := w.Write([]byte(`{"status":"draining"}`)); err != nil {
			slog.DebugContext(ctx, "server: failed writing draining response")
		}

		return
	}

	body := make(map[string]any)
	checks := make(map[string]any)
	details := make([]map[string]any, 0, len(h.checks))
	body["checks"] = checks
	isOk := true

	if h.checkResultCache != nil {
		for i, check := range h.checks {
			detail, ok := checkDetail(i, check, h.checkResultCache)
			details = append(details, detail)

			if !ok {
				isOk = false
			}
		}
	} else {
		isOk = false
		body["output"] = "missing check cache"
	}

	checks["tilegroxy:checks"] = details

	if isOk {
		body["status"] = "ok"
	} else {
		body["status"] = "error"
	}

	version, gitRef, _ := static.GetVersionInformation()
	body["version"] = version
	body["releaseId"] = gitRef

	data, err := json.Marshal(body)

	if err != nil {
		slog.ErrorContext(ctx, "Unable to write health", "error", err, "stack", string(debug.Stack()))
		isOk = false
	}

	w.Header().Add("Content-Type", "application/json+health")

	if isOk {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	_, err = w.Write(data)

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Unable to write to health request due to %v", err))
	}
}

func SetupHealth(ctx context.Context, cfg *config.Config, layerGroup *layer.LayerGroup) (func(context.Context) error, func(), error) {
	h := cfg.Server.Health

	slog.InfoContext(ctx, fmt.Sprintf("Initializing health subsystem with %v checks on %v:%v", len(h.Checks), h.Host, h.Port))

	var err error
	var callback func(context.Context) error
	checkResultCache := sync.Map{}
	var checks []health.HealthCheck
	noopDrain := func() {}

	if len(h.Checks) > 0 {
		checks, callback, err = setupCheckRoutines(ctx, h, layerGroup, cfg, &checkResultCache)
		if err != nil {
			return callback, noopDrain, err
		}
	}

	callback2, startDraining, err := setupHealthEndpoints(ctx, h, checks, &checkResultCache)

	if startDraining == nil {
		startDraining = noopDrain
	}

	if callback2 != nil {
		if callback != nil {
			return func(ctx context.Context) error {
				return errors.Join(callback(ctx), callback2(ctx))
			}, startDraining, err
		}

		return callback2, startDraining, err
	}

	return callback, startDraining, err
}

func setupHealthEndpoints(ctx context.Context, h config.HealthConfig, checks []health.HealthCheck, checkResultCache *sync.Map) (func(context.Context) error, func(), error) {
	srvErr := make(chan error, 1)
	httpHostPort := net.JoinHostPort(h.Host, strconv.Itoa(h.Port))

	draining := &atomic.Bool{}

	r := http.ServeMux{}
	r.HandleFunc("/", handleNoContent)
	r.Handle("/health", healthHandler{checks, checkResultCache, draining})

	// Health has to keep answering through the drain window, so its requests hang off the
	// un-signalled root rather than the signal context that cancels when shutdown begins
	healthRootCtx := context.WithoutCancel(ctx)

	srv := &http.Server{
		Addr:              httpHostPort,
		BaseContext:       func(_ net.Listener) context.Context { return healthRootCtx },
		Handler:           &r,
		ReadHeaderTimeout: time.Second,
	}

	go func() { srvErr <- srv.ListenAndServe() }()

	var err error

	// Give srv a little breathing room to try to start up
	select {
	case err = <-srvErr:
	case <-time.After(startupWaitTime):
	}

	return srv.Shutdown, func() { draining.Store(true) }, err
}

func setupCheckRoutines(ctx context.Context, h config.HealthConfig, layerGroup *layer.LayerGroup, cfg *config.Config, checkResultCache *sync.Map) ([]health.HealthCheck, func(context.Context) error, error) {
	checks := make([]health.HealthCheck, 0, len(h.Checks))
	var callback func(context.Context) error
	tickers := make([]*time.Ticker, 0, len(h.Checks))
	exitChannels := make([]chan struct{}, 0, len(h.Checks))

	for _, checkCfg := range h.Checks {
		hc, err := health.ConstructHealthCheck(checkCfg, layerGroup, cfg)
		if err != nil {
			return nil, nil, err
		}
		checks = append(checks, hc)
	}

	for i, check := range checks {
		delay := check.GetDelay()

		if delay > math.MaxInt32 {
			delay = math.MaxInt32
		}
		ttl := time.Second * time.Duration(delay) // #nosec G115

		ticker := time.NewTicker(ttl)
		done := make(chan struct{})
		tickers = append(tickers, ticker)
		exitChannels = append(exitChannels, done)

		tickCheck(ctx, i, check, ttl, checkResultCache)

		go func() {
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					tickCheck(ctx, i, check, ttl, checkResultCache)
				}
			}
		}()
	}

	// Stopping broadcasts a close rather than sending: a send on an unbuffered channel deadlocks
	// forever once the ticker goroutine has already exited, which two callers of the same shutdown
	// func can reach. The Once keeps that second call from panicking on a closed channel.
	var stopOnce sync.Once

	callback = func(ctx context.Context) error {
		stopOnce.Do(func() {
			slog.InfoContext(ctx, "Terminating health subsystem")

			for _, ticker := range tickers {
				ticker.Stop()
			}
			for _, channel := range exitChannels {
				close(channel)
			}
		})

		return nil
	}

	return checks, callback, nil
}

func tickCheck(ctx context.Context, i int, check health.HealthCheck, ttl time.Duration, checkResultCache *sync.Map) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "Unexpected panic during health check!", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	slog.Log(ctx, config.LevelTrace, fmt.Sprintf("health check %v running", i))

	err := check.Check(ctx)

	if err != nil {
		slog.WarnContext(ctx, "health check failed", "error", err)
	}

	result := CheckResult{err: err, timestamp: time.Now(), ttl: ttl}

	checkResultCache.Store(i, result)
}
