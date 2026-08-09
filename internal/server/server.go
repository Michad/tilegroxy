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
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/layer"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/crypto/acme/autocert"

	"github.com/gorilla/handlers"
)

func handleNoContent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// This is just here to allow tests to specify a different signal to send to kill the webserver
// not useful in practice due to OS-specific nature of signals
var InterruptFlags = []os.Signal{os.Interrupt}

// onReloadPtrSet is a test-only hook invoked right after ListenAndServe publishes the reload
// callback through its reloadPtr argument. That write happens on whatever goroutine runs
// ListenAndServe, so a test driving the server from another goroutine has no happens-before edge
// permitting it to read the result; calling into this hook supplies one. Always nil in production,
// and only ever assigned by a test before it starts a server.
var onReloadPtrSet func()

// reloadEntitiesFunc names the reload callback shape so it can be spelled out inside
// ListenAndServe's body, where the "config" parameter name shadows the config package.
type reloadEntitiesFunc = func(*config.Config, *layer.LayerGroup, authentication.Authentication) error

// healthReloader tears down the current health subsystem generation and builds a fresh one against
// the new LayerGroup, storing the new generation's shutdown func through healthShutdown. Declared
// at package level - rather than as a closure inside ListenAndServe - purely so its parameter
// types can spell out *config.Config, which ListenAndServe's own "config" parameter shadows within
// its body.
//
// The whole teardown-then-rebuild sequence runs under healthMutex. The mutex is not merely
// guarding the pointer: concurrent reloads are reachable in practice (pkg/config dispatches each
// config-change event in its own goroutine), and without serialization two reloads would both read
// the same old shutdown func, both tear the same generation down, and both race to bind the health
// port. Holding the lock across the whole sequence makes reloads strictly sequential.
func healthReloader(ctx context.Context, cfg *config.Config, layerGroup *layer.LayerGroup, healthMutex *sync.Mutex, healthShutdown *func(context.Context) error) error {
	if !cfg.Server.Health.Enabled {
		return nil
	}

	healthMutex.Lock()
	defer healthMutex.Unlock()

	// The old generation's HTTP server must be shut down (freeing its listener) before the new
	// one binds - health host/port normally doesn't change between reloads, so binding first
	// would collide with the still-listening old server.
	oldHealthShutdown := *healthShutdown
	*healthShutdown = nil

	if oldHealthShutdown != nil {
		if err := oldHealthShutdown(context.Background()); err != nil {
			slog.WarnContext(ctx, fmt.Sprintf("Error shutting down previous health generation: %v", err))
		}
	}

	newHealthShutdown, err := SetupHealth(ctx, cfg, layerGroup)

	// SetupHealth can return a non-nil shutdown func alongside an error (it partially builds the
	// subsystem before failing), so always record whatever it handed back. On failure that means
	// the pointer holds a func that tears down the partial generation instead of a stale pointer
	// to the already-shut-down previous generation, which the final shutdown in ListenAndServe
	// would otherwise invoke. We deliberately do not try to resurrect the previous generation: it
	// was built from the old config against the old LayerGroup and re-running SetupHealth on it
	// could fail just as easily, leaving no clear recovery point. Instead the error propagates so
	// the caller can surface a loud, actionable failure, and the next successful reload rebuilds
	// health cleanly.
	*healthShutdown = newHealthShutdown

	if err != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("Failed to rebuild health subsystem on reload, health endpoint is DOWN until config is fixed and reloaded again: %v", err))
		return err
	}

	return nil
}

// makeCombinedReloadFunc builds the reload callback ListenAndServe hands back to its caller: it
// runs the tile handler's own reload (swapping to the new LayerGroup) and then rebuilds the
// health subsystem against that same new LayerGroup, so health checks stop being permanently
// pinned to the LayerGroup instance from startup.
func makeCombinedReloadFunc(ctx context.Context, handlerReloadFunc reloadEntitiesFunc, healthMutex *sync.Mutex, healthShutdown *func(context.Context) error) reloadEntitiesFunc {
	return func(cfg2 *config.Config, layerGroup2 *layer.LayerGroup, auth2 authentication.Authentication) error {
		if err := handlerReloadFunc(cfg2, layerGroup2, auth2); err != nil {
			return err
		}

		return healthReloader(ctx, cfg2, layerGroup2, healthMutex, healthShutdown)
	}
}

func setupHandlers(cfg *config.Config, layerGroup *layer.LayerGroup, auth authentication.Authentication) (http.Handler, func(*config.Config, *layer.LayerGroup, authentication.Authentication) error, error) {
	r := http.ServeMux{}

	var myRootHandler http.Handler
	var myTileHandler http.Handler
	var myDocumentationHandler http.Handler
	entities := reloadableEntities{config: cfg, auth: auth, layerGroup: layerGroup}
	myDefaultHandler := defaultHandler{entities}

	if cfg.Server.Production {
		myRootHandler = http.HandlerFunc(handleNoContent)
	} else {
		myRootHandler = &myDefaultHandler

		if cfg.Server.DocsPath != "" {
			myDocumentationHandler = &documentationHandler{myDefaultHandler}
		}
	}

	tilePath := cfg.Server.RootPath + cfg.Server.TilePath + "/{layer}/{z}/{x}/{y}"
	docsPath := cfg.Server.RootPath + cfg.Server.DocsPath + "/{path...}"
	handler, err := newTileHandler(entities)
	if err != nil {
		return nil, nil, err
	}

	myTileHandler = &handler

	reloadFunc := func(cfg2 *config.Config, layerGroup2 *layer.LayerGroup, auth2 authentication.Authentication) error {
		entities2 := reloadableEntities{config: cfg2, auth: auth2, layerGroup: layerGroup2}

		handler.reloadEntities(entities2)

		return nil
	}

	if cfg.Telemetry.Enabled {
		myRootHandler = otelhttp.NewHandler(myRootHandler, cfg.Server.RootPath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))
		myTileHandler = otelhttp.NewHandler(myTileHandler, tilePath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))

		if myDocumentationHandler != nil {
			myDocumentationHandler = otelhttp.NewHandler(myDocumentationHandler, docsPath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))
		}
	}

	r.Handle(cfg.Server.RootPath, myRootHandler)
	r.Handle(tilePath, myTileHandler)
	r.Handle(tilePath+"/", myTileHandler)

	if myDocumentationHandler != nil {
		r.Handle(docsPath, myDocumentationHandler)
	}

	var rootHandler http.Handler

	rootHandler = &r

	if cfg.Server.Gzip {
		rootHandler = handlers.CompressHandler(rootHandler)
	}

	if cfg.Server.Timeout > math.MaxInt32 {
		cfg.Server.Timeout = math.MaxInt32
	}

	rootHandler = httpContextHandler{rootHandler, cfg.Error}
	rootHandler = http.TimeoutHandler(rootHandler, time.Duration(cfg.Server.Timeout)*time.Second, cfg.Error.Messages.Timeout) // #nosec G115
	rootHandler, err = configureAccessLogging(cfg.Logging.Access, cfg.Error.Messages, rootHandler)

	if err != nil {
		return nil, nil, err
	}

	return rootHandler, reloadFunc, nil
}

func listenAndServeTLS(config *config.Config, srvErr chan error, srv *http.Server) {
	httpPort := config.Server.Encrypt.HTTPPort
	httpHostPort := net.JoinHostPort(config.Server.BindHost, strconv.Itoa(httpPort))

	if config.Server.Encrypt.Certificate != "" && config.Server.Encrypt.KeyFile != "" {
		if httpPort != 0 {
			srv := &http.Server{
				Addr:              httpHostPort,
				Handler:           httpRedirectHandler{protoAndHost: "https://" + config.Server.Encrypt.Domain},
				ReadHeaderTimeout: time.Second,
			}

			go func() {
				srvErr <- srv.ListenAndServe()
			}()
		}

		srvErr <- srv.ListenAndServeTLS(config.Server.Encrypt.Certificate, config.Server.Encrypt.KeyFile)
	} else {
		// Let's Encrypt workflow

		cacheDir := "certs"
		if config.Server.Encrypt.Cache != "" {
			cacheDir = config.Server.Encrypt.Cache
		}

		certManager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(config.Server.Encrypt.Domain),
			Cache:      autocert.DirCache(cacheDir),
		}

		if httpPort != 0 {
			srv := &http.Server{
				Addr:              httpHostPort,
				Handler:           certManager.HTTPHandler(nil),
				ReadHeaderTimeout: time.Second,
			}

			go func() { srvErr <- srv.ListenAndServe() }()
		}

		srv.TLSConfig = certManager.TLSConfig()

		srvErr <- srv.ListenAndServeTLS("", "")
	}
}

func ListenAndServe(config *config.Config, layerGroup *layer.LayerGroup, auth authentication.Authentication, reloadPtr *func(*config.Config, *layer.LayerGroup, authentication.Authentication) error) error {
	if config.Server.Encrypt != nil && config.Server.Encrypt.Domain == "" {
		return fmt.Errorf(config.Error.Messages.ParamRequired, "server.encrypt.domain")
	}

	rootHandler, handlerReloadFunc, err := setupHandlers(config, layerGroup, auth)

	if err != nil {
		return err
	}

	err = configureMainLogging(config)

	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(pkg.BackgroundContext(), InterruptFlags...)
	defer stop()

	// healthMutex guards healthShutdown, which the reload callback below replaces with a fresh
	// generation's shutdown func every time config is reloaded.
	var healthMutex sync.Mutex
	var healthShutdown func(context.Context) error

	if config.Server.Health.Enabled {
		healthShutdown, err = SetupHealth(ctx, config, layerGroup)

		if err != nil {
			return err
		}
	}

	if reloadPtr != nil {
		*reloadPtr = makeCombinedReloadFunc(ctx, handlerReloadFunc, &healthMutex, &healthShutdown)
	}

	// reloadPtr is written from whatever goroutine runs ListenAndServe, so a caller on another
	// goroutine has no happens-before edge letting it safely read the result. onReloadPtrSet is a
	// test-only hook supplying that edge; it is nil in production.
	if onReloadPtrSet != nil {
		onReloadPtrSet()
	}

	var otelShutdown func(context.Context) error

	if config.Telemetry.Enabled {
		// Set up OpenTelemetry.
		otelShutdown, err = setupOTELSDK(ctx)
		if err != nil {
			return err
		}
	}

	srv := &http.Server{
		Addr:              config.Server.BindHost + ":" + strconv.Itoa(config.Server.Port),
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
		Handler:           rootHandler,
		ReadHeaderTimeout: time.Second,
	}

	srvErr := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				srvErr <- fmt.Errorf("unexpected server error %v \n %v", r, string(debug.Stack()))
			}
		}()

		slog.InfoContext(context.Background(), "Binding...")

		if config.Server.Encrypt != nil {
			listenAndServeTLS(config, srvErr, srv)
		} else {
			srvErr <- srv.ListenAndServe()
		}
	}()

	select {
	case err = <-srvErr:
		return err
	case <-ctx.Done():
		stop()
	}

	err = srv.Shutdown(context.Background())

	if otelShutdown != nil {
		err = errors.Join(err, otelShutdown(context.Background()))
	}

	healthMutex.Lock()
	finalHealthShutdown := healthShutdown
	healthMutex.Unlock()

	if finalHealthShutdown != nil {
		err = errors.Join(err, finalHealthShutdown(context.Background()))
	}

	return err
}
