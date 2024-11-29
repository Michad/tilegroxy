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
	if reloadPtr != nil {
		*reloadPtr = handlerReloadFunc
	}

	if err != nil {
		return err
	}

	err = configureMainLogging(config)

	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(pkg.BackgroundContext(), InterruptFlags...)
	defer stop()

	var healthShutdown func(context.Context) error

	if config.Server.Health.Enabled {
		healthShutdown, err = SetupHealth(ctx, config, layerGroup)

		if err != nil {
			return err
		}
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

	if healthShutdown != nil {
		err = errors.Join(err, healthShutdown(context.Background()))
	}

	return err
}
