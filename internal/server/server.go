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
	"syscall"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/crypto/acme/autocert"

	"github.com/gorilla/handlers"
)

func handleNoContent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// Signals that begin a graceful shutdown. SIGTERM is what container runtimes send on stop;
// without it the process dies on Go's default disposition and nothing gets flushed. Tests
// override this to send a signal they control.
var InterruptFlags = []os.Signal{os.Interrupt, syscall.SIGTERM}

// onReloadPtrSet is a test-only hook invoked right after ListenAndServe publishes the reload
// callback through its reloadPtr argument, giving a test on another goroutine a happens-before
// edge for reading it. Nil in production.
var onReloadPtrSet func()

// reloadEntitiesFunc names the reload callback shape so it can be spelled out inside
// ListenAndServe's body, where the "config" parameter name shadows the config package.
type reloadEntitiesFunc = func(*config.Config, *entities.Entities) error

// healthReloader tears down the current health subsystem generation and builds a fresh one against
// the new generation's LayerGroup. Declared at package level rather than as a closure inside
// ListenAndServe so its parameter types can name *config.Config, which that function's own
// "config" parameter shadows.
//
// The mutex is held across the whole teardown-then-rebuild rather than just around the pointer.
// pkg/config dispatches each config-change event on its own goroutine, so two concurrent reloads
// would otherwise both tear down the same generation and race to bind the health port.
func healthReloader(ctx context.Context, cfg *config.Config, ent *entities.Entities, healthMutex *sync.Mutex, healthShutdown *func(context.Context) error, healthDrain *func(), draining *bool) error {
	if !cfg.Server.Health.Enabled {
		return nil
	}

	healthMutex.Lock()
	defer healthMutex.Unlock()

	oldHealthShutdown := *healthShutdown
	*healthShutdown = nil
	*healthDrain = nil

	// The old generation has to free its listener before the new one binds, since the health
	// host/port rarely changes between reloads.
	if oldHealthShutdown != nil {
		if err := oldHealthShutdown(context.Background()); err != nil {
			slog.WarnContext(ctx, fmt.Sprintf("Error shutting down previous health generation: %v", err))
		}
	}

	newHealthShutdown, newHealthDrain, err := SetupHealth(ctx, cfg, ent.LayerGroup)

	// SetupHealth returns a non-nil shutdown func alongside an error when it fails partway, so
	// record whatever it hands back either way. Otherwise the pointer keeps referencing the
	// already-shut-down previous generation and ListenAndServe's final shutdown calls it again.
	// The previous generation is not resurrected: it was built against the old config and
	// LayerGroup, so rebuilding it could fail just as easily. The error propagates instead, and
	// the next successful reload brings health back.
	*healthShutdown = newHealthShutdown
	*healthDrain = newHealthDrain

	// If shutdown already started draining before this reload landed, the new generation must not
	// come up reporting ready: a reload racing with shutdown must not reopen the window that
	// draining closed.
	if draining != nil && *draining {
		newHealthDrain()
	}

	if err != nil {
		slog.ErrorContext(ctx, fmt.Sprintf("Failed to rebuild health subsystem on reload, health endpoint is DOWN until config is fixed and reloaded again: %v", err))
		return err
	}

	return nil
}

// makeCombinedReloadFunc builds the reload callback ListenAndServe hands back to its caller: it
// swaps the tile handlers to the new entities, then rebuilds the health subsystem against that
// same generation so health checks aren't left pinned to the LayerGroup from startup.
func makeCombinedReloadFunc(ctx context.Context, handlerReloadFunc reloadEntitiesFunc, healthMutex *sync.Mutex, healthShutdown *func(context.Context) error, healthDrain *func(), draining *bool) reloadEntitiesFunc {
	return func(cfg2 *config.Config, ent2 *entities.Entities) error {
		if err := handlerReloadFunc(cfg2, ent2); err != nil {
			return err
		}

		return healthReloader(ctx, cfg2, ent2, healthMutex, healthShutdown, healthDrain, draining)
	}
}

// setupHandlers builds the HTTP handlers. The returned accessor yields whichever generation of entities is
// currently serving, which is what shutdown needs to release after a hot reload has swapped generations
func setupHandlers(cfg *config.Config, ent *entities.Entities) (http.Handler, reloadEntitiesFunc, func() *entities.Entities, *generationRegistry, func() error, error) {
	r := http.ServeMux{}

	var myRootHandler http.Handler
	var myTileHandler http.Handler
	var myDocumentationHandler http.Handler
	registry := newGenerationRegistry()
	firstGen := newGeneration(ent)
	registry.add(firstGen)
	reloadable := newReloadableEntities(cfg, ent, firstGen)
	myDefaultHandler := defaultHandler{reloadable}

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
	handler, err := newTileHandler(reloadable)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	myTileHandler = &handler

	tileJSON := setupTileJSONHandlers(cfg, reloadable)

	reloadFunc := func(cfg2 *config.Config, ent2 *entities.Entities) error {
		gen := newGeneration(ent2)
		registry.add(gen)
		handler.reloadEntities(newReloadableEntities(cfg2, ent2, gen))
		tileJSON.reloadEntities(cfg2, ent2, gen)

		return nil
	}

	if cfg.Telemetry.Enabled {
		myRootHandler = otelhttp.NewHandler(myRootHandler, cfg.Server.RootPath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))
		myTileHandler = otelhttp.NewHandler(myTileHandler, tilePath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))

		if myDocumentationHandler != nil {
			myDocumentationHandler = otelhttp.NewHandler(myDocumentationHandler, docsPath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))
		}

		tileJSON.wrapWithTelemetry()
	}

	r.Handle(cfg.Server.RootPath, myRootHandler)
	r.Handle(tilePath, myTileHandler)
	r.Handle(tilePath+"/", myTileHandler)

	if myDocumentationHandler != nil {
		r.Handle(docsPath, myDocumentationHandler)
	}

	tileJSON.registerRoutes(&r)

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
	var closeAccessLog func() error
	rootHandler, closeAccessLog, err = configureAccessLogging(cfg.Logging.Access, cfg.Error.Messages, rootHandler)

	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	return rootHandler, reloadFunc, handler.currentEntities, registry, closeAccessLog, nil
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

func ListenAndServe(config *config.Config, ent *entities.Entities, reloadPtr *func(*config.Config, *entities.Entities) error) error {
	if config.Server.Encrypt != nil && config.Server.Encrypt.Domain == "" {
		return fmt.Errorf(config.Error.Messages.ParamRequired, "server.encrypt.domain")
	}

	rootHandler, handlerReloadFunc, _, registry, closeAccessLog, err := setupHandlers(config, ent)

	if err != nil {
		return err
	}

	closeMainLog, err := configureMainLogging(config)

	if err != nil {
		return err
	}

	// Requests hang off the un-signalled root. Deriving them from the signal context instead would
	// cancel every in-flight request the moment SIGTERM lands, which TimeoutHandler turns into an
	// empty 503, defeating both the drain delay and the server's own graceful shutdown
	rootCtx := pkg.BackgroundContext()

	ctx, stop := signal.NotifyContext(rootCtx, InterruptFlags...)
	defer stop()

	var healthMutex sync.Mutex
	var healthShutdown func(context.Context) error
	var healthDrain func()
	// Guarded by healthMutex, same as healthShutdown and healthDrain. Recorded here so a reload
	// that rebuilds the health subsystem after shutdown has begun brings the new generation up
	// already draining, instead of reopening the readiness window shutdown just closed.
	var draining bool

	if config.Server.Health.Enabled {
		healthShutdown, healthDrain, err = SetupHealth(ctx, config, ent.LayerGroup)

		if err != nil {
			return err
		}
	}

	if reloadPtr != nil {
		*reloadPtr = makeCombinedReloadFunc(ctx, handlerReloadFunc, &healthMutex, &healthShutdown, &healthDrain, &draining)
	}

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
		BaseContext:       func(_ net.Listener) context.Context { return rootCtx },
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

	return runShutdown(context.Background(), newShutdownBudget(config), buildShutdownPhases(shutdownDeps{
		healthMutex:    &healthMutex,
		draining:       &draining,
		healthShutdown: &healthShutdown,
		healthDrain:    &healthDrain,
		srv:            srv,
		registry:       registry,
		otelShutdown:   otelShutdown,
		closeAccessLog: closeAccessLog,
		closeMainLog:   closeMainLog,
	}))
}

// shutdownDeps is what the teardown phases need from ListenAndServe. Gathered into one struct so
// the phase wiring can live outside that function
type shutdownDeps struct {
	healthMutex    *sync.Mutex
	draining       *bool
	healthShutdown *func(context.Context) error
	healthDrain    *func()
	srv            *http.Server
	registry       *generationRegistry
	otelShutdown   func(context.Context) error
	closeAccessLog func() error
	closeMainLog   func() error
}

func buildShutdownPhases(d shutdownDeps) shutdownPhases {
	// Read under the mutex healthReloader writes them under, so a reload landing as shutdown
	// starts can't be observed half-applied
	d.healthMutex.Lock()
	finalHealthShutdown := *d.healthShutdown
	finalHealthDrain := *d.healthDrain
	d.healthMutex.Unlock()

	return shutdownPhases{
		drain: func() {
			// Set under the mutex healthReloader reads, so a reload landing mid-shutdown brings
			// the rebuilt health subsystem up already draining
			d.healthMutex.Lock()
			*d.draining = true
			d.healthMutex.Unlock()

			if finalHealthDrain != nil {
				finalHealthDrain()
			}
		},
		server:      d.srv.Shutdown,
		generations: d.registry.closeAll,
		health: func(shutdownCtx context.Context) error {
			if finalHealthShutdown == nil {
				return nil
			}

			return finalHealthShutdown(shutdownCtx)
		},
		otel: func(shutdownCtx context.Context) error {
			if d.otelShutdown == nil {
				return nil
			}

			return d.otelShutdown(shutdownCtx)
		},
		logs: func() {
			if err := d.closeAccessLog(); err != nil {
				slog.WarnContext(context.Background(), fmt.Sprintf("Error closing access log: %v", err))
			}

			if err := d.closeMainLog(); err != nil {
				slog.WarnContext(context.Background(), fmt.Sprintf("Error closing main log: %v", err))
			}
		},
	}
}
