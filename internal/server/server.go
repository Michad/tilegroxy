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

func setupHandlers(config *config.Config, layerGroup *layer.LayerGroup, auth authentication.Authentication) (http.Handler, error) {
	r := http.ServeMux{}

	var myRootHandler http.Handler
	var myTileHandler http.Handler
	var myDocumentationHandler http.Handler
	defaultHandler := defaultHandler{config: config, auth: auth, layerGroup: layerGroup}

	if config.Server.Production {
		myRootHandler = http.HandlerFunc(handleNoContent)
	} else {
		myRootHandler = &defaultHandler

		if config.Server.DocsPath != "" {
			myDocumentationHandler = &documentationHandler{defaultHandler}
		}
	}

	tilePath := config.Server.RootPath + config.Server.TilePath + "/{layer}/{z}/{x}/{y}"
	docsPath := config.Server.RootPath + config.Server.DocsPath + "/{path...}"
	handler, err := newTileHandler(defaultHandler)
	if err != nil {
		return nil, err
	}

	myTileHandler = &handler

	if config.Telemetry.Enabled {
		myRootHandler = otelhttp.NewHandler(myRootHandler, config.Server.RootPath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))
		myTileHandler = otelhttp.NewHandler(myTileHandler, tilePath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))

		if myDocumentationHandler != nil {
			myDocumentationHandler = otelhttp.NewHandler(myDocumentationHandler, docsPath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))
		}
	}

	r.Handle(config.Server.RootPath, myRootHandler)
	r.Handle(tilePath, myTileHandler)
	r.Handle(tilePath+"/", myTileHandler)

	if myDocumentationHandler != nil {
		r.Handle(docsPath, myDocumentationHandler)
	}

	var rootHandler http.Handler

	rootHandler = &r

	if config.Server.Gzip {
		rootHandler = handlers.CompressHandler(rootHandler)
	}

	rootHandler = httpContextHandler{rootHandler, config.Error}
	rootHandler = http.TimeoutHandler(rootHandler, time.Duration(config.Server.Timeout)*time.Second, config.Error.Messages.Timeout)
	rootHandler, err = configureAccessLogging(config.Logging.Access, config.Error.Messages, rootHandler)

	if err != nil {
		return nil, err
	}

	return rootHandler, nil
}

func listenAndServeTLS(config *config.Config, srvErr chan error, srv *http.Server) {
	httpPort := config.Server.Encrypt.HTTPPort
	httpHostPort := net.JoinHostPort(config.Server.BindHost, strconv.Itoa(httpPort))

	if config.Server.Encrypt.Certificate != "" && config.Server.Encrypt.KeyFile != "" {
		if httpPort != 0 {
			go func() {
				srvErr <- http.ListenAndServe(httpHostPort, httpRedirectHandler{protoAndHost: "https://" + config.Server.Encrypt.Domain})
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
			go func() { srvErr <- http.ListenAndServe(httpHostPort, certManager.HTTPHandler(nil)) }()
		}

		srv.TLSConfig = certManager.TLSConfig()

		srvErr <- srv.ListenAndServeTLS("", "")
	}
}

func ListenAndServe(config *config.Config, layerGroup *layer.LayerGroup, auth authentication.Authentication) error {
	if config.Server.Encrypt != nil && config.Server.Encrypt.Domain == "" {
		return fmt.Errorf(config.Error.Messages.ParamRequired, "server.encrypt.domain")
	}

	rootHandler, err := setupHandlers(config, layerGroup, auth)

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
		Addr:        config.Server.BindHost + ":" + strconv.Itoa(config.Server.Port),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
		Handler:     rootHandler,
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
