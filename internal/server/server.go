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
	"syscall"
	"time"

	"github.com/Michad/tilegroxy/internal/entities"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/crypto/acme/autocert"

	"github.com/gorilla/handlers"
)

func ValidateConfig(cfg *config.Config) error {
	if cfg.Server.Encrypt != nil && cfg.Server.Encrypt.Domain == "" {
		return fmt.Errorf(cfg.Error.Messages.ParamRequired, "server.encrypt.domain")
	}

	if cfg.Server.Timeout > math.MaxInt16 {
		return fmt.Errorf(cfg.Error.Messages.RangeError, "server.timeout", 0, math.MaxInt16)
	}

	if err := validateCORS(cfg.Server.CORS, cfg.Error.Messages); err != nil {
		return err
	}

	return validateAllCacheControl(cfg)
}

func handleNoContent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// Begin a graceful shutdown
var InterruptFlags = []os.Signal{os.Interrupt, syscall.SIGTERM}

type reloadEntitiesFunc = func(*config.Config, *entities.Entities) error

// The root handler plus the generations it serves from
type handlerSetup struct {
	root           http.Handler
	first          *generation
	gens           *generationHolder
	registry       *generationRegistry
	closeAccessLog func() error
}

// The new config is deliberately ignored since every handler-visible section is non-reloadable
func (s handlerSetup) reload(_ *config.Config, ent *entities.Entities) error {
	slog.WarnContext(pkg.BackgroundContext(), "Requesting to refresh entities from configuration")

	gen := s.first.succeededBy(ent)
	s.registry.add(gen)
	s.gens.reload(gen)

	slog.WarnContext(pkg.BackgroundContext(), "Completed refreshing entities from configuration")

	return nil
}

func setupHandlers(cfg *config.Config, ent *entities.Entities) (handlerSetup, error) {
	registry := newGenerationRegistry()
	first := newGeneration(cfg, ent)
	registry.add(first)
	gens := newGenerationHolder(first)

	mux, err := newRouter(cfg, gens)
	if err != nil {
		return handlerSetup{}, err
	}

	root, closeAccessLog, err := wrapRootHandler(cfg, mux)
	if err != nil {
		return handlerSetup{}, err
	}

	return handlerSetup{root: root, first: first, gens: gens, registry: registry, closeAccessLog: closeAccessLog}, nil
}

func newRouter(cfg *config.Config, gens *generationHolder) (*http.ServeMux, error) {
	instrument := func(h http.Handler, route string) http.Handler {
		if !cfg.Telemetry.Enabled {
			return h
		}

		return otelhttp.NewHandler(h, route, otelhttp.WithMessageEvents(otelhttp.WriteEvents))
	}

	r := &http.ServeMux{}

	tilePath := cfg.Server.RootPath + cfg.Server.TilePath + "/{layer}/{z}/{x}/{y}"

	tile, err := newTileHandler(gens)
	if err != nil {
		return nil, err
	}

	tiles := instrument(&tile, tilePath)
	r.Handle(tilePath, tiles)
	r.Handle(tilePath+"/", tiles)

	if cfg.Server.Production {
		r.Handle(cfg.Server.RootPath, instrument(http.HandlerFunc(handleNoContent), cfg.Server.RootPath))
	} else {
		root := defaultHandler{gens}
		r.Handle(cfg.Server.RootPath, instrument(&root, cfg.Server.RootPath))

		if cfg.Server.DocsPath != "" {
			docsPath := cfg.Server.RootPath + cfg.Server.DocsPath + "/{path...}"
			r.Handle(docsPath, instrument(&documentationHandler{root}, docsPath))
		}

		previewPath := cfg.Server.RootPath + "preview/{layer}"
		r.Handle(previewPath, instrument(newPreviewHandler(gens), previewPath))
	}

	tileJSON := setupTileJSONHandlers(cfg, gens)

	if cfg.Telemetry.Enabled {
		tileJSON.wrapWithTelemetry()
	}

	tileJSON.registerRoutes(r)

	return r, nil
}

func wrapRootHandler(cfg *config.Config, mux *http.ServeMux) (http.Handler, func() error, error) {
	var rootHandler http.Handler = mux

	if cfg.Server.Gzip {
		rootHandler = handlers.CompressHandler(rootHandler)
	}

	rootHandler = httpContextHandler{rootHandler, cfg.Error}
	rootHandler = newTimeoutHandler(rootHandler, time.Duration(cfg.Server.Timeout)*time.Second, &cfg.Error) // #nosec G115 -- config normalization clamps the timeout to MaxInt32 seconds

	if cfg.Server.CORS.Enabled {
		rootHandler = corsHandler{rootHandler, cfg.Server.CORS}
	}

	return configureAccessLogging(cfg.Logging.Access, cfg.Error.Messages, rootHandler)
}

func listenAndServeTLS(cfg *config.Config, srvErr chan error, srv *http.Server) {
	httpPort := cfg.Server.Encrypt.HTTPPort
	httpHostPort := net.JoinHostPort(cfg.Server.BindHost, strconv.Itoa(httpPort))

	if cfg.Server.Encrypt.Certificate != "" && cfg.Server.Encrypt.KeyFile != "" {
		if httpPort != 0 {
			srv := &http.Server{
				Addr:              httpHostPort,
				Handler:           httpRedirectHandler{host: cfg.Server.Encrypt.Domain},
				ReadHeaderTimeout: time.Second,
			}

			go func() {
				srvErr <- srv.ListenAndServe()
			}()
		}

		srvErr <- srv.ListenAndServeTLS(cfg.Server.Encrypt.Certificate, cfg.Server.Encrypt.KeyFile)
	} else {
		// Let's Encrypt workflow

		cacheDir := "certs"
		if cfg.Server.Encrypt.Cache != "" {
			cacheDir = cfg.Server.Encrypt.Cache
		}

		certManager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.Server.Encrypt.Domain),
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

// Returns closers in the order shutdown needs them. setupHandlers already sets up the access log
func configureLogging(cfg *config.Config) (func() error, func() error, error) {
	closeMainLog, err := configureMainLogging(cfg)

	if err != nil {
		return nil, nil, err
	}

	closeAuditLog, err := configureAuditLogging(cfg.Logging.Audit, cfg.Error.Messages)

	if err != nil {
		return nil, nil, errors.Join(err, closeMainLog())
	}

	return closeMainLog, closeAuditLog, nil
}

func newHTTPServer(rootCtx context.Context, cfg *config.Config, rootHandler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Server.BindHost + ":" + strconv.Itoa(cfg.Server.Port),
		BaseContext:       func(_ net.Listener) context.Context { return rootCtx },
		Handler:           rootHandler,
		ReadHeaderTimeout: time.Second,
		// Backstop for clients that stop reading, which timeoutHandler can't bound. Doubled so it only fires after Server.Timeout
		WriteTimeout: 2 * time.Duration(cfg.Server.Timeout) * time.Second, // #nosec G115 -- operator-supplied timeout in seconds, far below int64 overflow range
		IdleTimeout:  2 * time.Duration(cfg.Server.Timeout) * time.Second, // #nosec G115 -- operator-supplied timeout in seconds, far below int64 overflow range
	}
}

// onReady, if set, receives the reload function once the server can accept reloads
func ListenAndServe(cfg *config.Config, ent *entities.Entities, onReady func(reloadEntitiesFunc)) error {
	if err := ValidateConfig(cfg); err != nil {
		return err
	}

	routes, err := setupHandlers(cfg, ent)

	if err != nil {
		return err
	}

	closeMainLog, closeAuditLog, err := configureLogging(cfg)

	if err != nil {
		return err
	}

	// Derived from the signal context, SIGTERM would cancel every in-flight request into a 503, defeating graceful drain
	rootCtx := pkg.BackgroundContext()

	ctx, stop := signal.NotifyContext(rootCtx, InterruptFlags...)
	defer stop()

	health := &healthSupervisor{}

	if err = health.Start(ctx, cfg, ent); err != nil {
		return err
	}

	if onReady != nil {
		// Health rebuilds against the same entities so its checks aren't pinned to the startup LayerGroup
		onReady(func(newCfg *config.Config, newEnt *entities.Entities) error {
			if err := health.Reload(ctx, newCfg, newEnt); err != nil {
				return err
			}

			return routes.reload(newCfg, newEnt)
		})
	}

	var otelShutdown func(context.Context) error

	if cfg.Telemetry.Enabled {
		otelShutdown, err = setupOTELSDK(ctx)
		if err != nil {
			return err
		}
	}

	srv := newHTTPServer(rootCtx, cfg, routes.root)

	srvErr := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				srvErr <- fmt.Errorf("unexpected server error %v \n %v", r, string(debug.Stack()))
			}
		}()

		slog.InfoContext(context.Background(), "Binding...")

		if cfg.Server.Encrypt != nil {
			listenAndServeTLS(cfg, srvErr, srv)
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

	return runShutdown(context.Background(), newShutdownBudget(cfg), buildShutdownPhases(shutdownDeps{
		health:         health,
		srv:            srv,
		registry:       routes.registry,
		otelShutdown:   otelShutdown,
		closeAccessLog: routes.closeAccessLog,
		closeMainLog:   closeMainLog,
		closeAuditLog:  closeAuditLog,
	}))
}

// Gathered into one struct so the phase wiring can live outside ListenAndServe
type shutdownDeps struct {
	health         *healthSupervisor
	srv            *http.Server
	registry       *generationRegistry
	otelShutdown   func(context.Context) error
	closeAccessLog func() error
	closeMainLog   func() error
	closeAuditLog  func() error
}

func buildShutdownPhases(d shutdownDeps) shutdownPhases {
	return shutdownPhases{
		drain:       d.health.Drain,
		server:      d.srv.Shutdown,
		generations: d.registry.closeAll,
		health:      d.health.Shutdown,
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

			// Audit closes last so events from the phases above still land in it
			if err := d.closeAuditLog(); err != nil {
				slog.WarnContext(context.Background(), fmt.Sprintf("Error closing audit log: %v", err))
			}
		},
	}
}
