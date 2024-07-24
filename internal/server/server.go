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
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"golang.org/x/crypto/acme/autocert"

	"github.com/gorilla/handlers"
)

func handleNoContent(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// This is just here to allow tests to specify a different signal to send to kill the webserver
// not useful in practice due to OS-specific nature of signals
var InterruptFlags = []os.Signal{os.Interrupt}

func errorVars(cfg *config.ErrorConfig, errorType pkg.TypeOfError) (int, slog.Level, string) {
	var status int
	var level slog.Level
	var imgPath string

	switch errorType {
	case pkg.TypeOfErrorAuth:
		level = slog.LevelDebug
		status = http.StatusUnauthorized
		imgPath = cfg.Images.Authentication
	case pkg.TypeOfErrorBounds:
		level = slog.LevelDebug
		status = http.StatusBadRequest
		imgPath = cfg.Images.OutOfBounds
	case pkg.TypeOfErrorProvider:
		level = slog.LevelInfo
		status = http.StatusInternalServerError
		imgPath = cfg.Images.Provider
	case pkg.TypeOfErrorBadRequest:
		level = slog.LevelDebug
		status = http.StatusBadRequest
		imgPath = cfg.Images.Other
	default:
		level = slog.LevelWarn
		status = http.StatusInternalServerError
		imgPath = cfg.Images.Other
	}

	if cfg.AlwaysOk {
		status = http.StatusOK
	}

	return status, level, imgPath
}

func writeError(ctx *pkg.RequestContext, w http.ResponseWriter, cfg *config.ErrorConfig, err error) {
	var te pkg.TypedError
	if errors.As(err, &te) {
		writeErrorMessage(ctx, w, cfg, te.Type(), te.Error(), te.External(cfg.Messages), debug.Stack())
	} else {
		writeErrorMessage(ctx, w, cfg, pkg.TypeOfErrorOther, te.Error(), fmt.Sprintf(cfg.Messages.ServerError, err), debug.Stack())
	}
}

func writeErrorMessage(ctx *pkg.RequestContext, w http.ResponseWriter, cfg *config.ErrorConfig, errorType pkg.TypeOfError, internalMessage string, externalMessage string, stack []byte) {
	status, level, imgPath := errorVars(cfg, errorType)

	slog.Log(ctx, level, internalMessage, "stack", string(stack))

	if cfg.Mode == config.ModeErrorPlainText {
		w.WriteHeader(status)
		w.Write([]byte(externalMessage))
	} else if cfg.Mode == config.ModeErrorImageHeader || cfg.Mode == config.ModeErrorImage {
		if cfg.Mode == config.ModeErrorImageHeader {
			w.Header().Add("x-error-message", externalMessage)
		}
		w.WriteHeader(status)

		img, err2 := images.GetStaticImage(imgPath)
		if img != nil {
			w.Write(*img)
		}

		if err2 != nil {
			slog.ErrorContext(ctx, err2.Error())
		}
	} else {
		w.WriteHeader(status)
	}
}

type slogContextHandler struct {
	slog.Handler
	keys []string
}

func (h slogContextHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, k := range h.keys {
		r.AddAttrs(slog.Attr{Key: strings.ToLower(k), Value: slog.AnyValue(ctx.Value(k))})
	}

	return h.Handler.Handle(ctx, r)
}

func makeLogFileWriter(path string, alsoStdOut bool) (io.Writer, error) {
	logFile, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)

	if err != nil {
		return nil, err
	}
	var out io.Writer

	if alsoStdOut {
		out = io.MultiWriter(os.Stdout, logFile)
	} else {
		out = logFile
	}
	return out, nil
}

func configureMainLogging(cfg *config.Config) error {

	var err error
	if cfg.Logging.Main.Console || len(cfg.Logging.Main.Path) > 0 {
		var out io.Writer
		if len(cfg.Logging.Main.Path) > 0 {
			out, err = makeLogFileWriter(cfg.Logging.Main.Path, cfg.Logging.Main.Console)
			if err != nil {
				return err
			}
		} else if cfg.Logging.Main.Console {
			out = os.Stdout
		} else {
			panic("Impossible logic error")
		}

		var level slog.Level
		custLogLevel, ok := config.CustomLogLevel[strings.ToLower(cfg.Logging.Main.Level)]

		if ok {
			level = custLogLevel
		} else {
			err := level.UnmarshalText([]byte(cfg.Logging.Main.Level))

			if err != nil {
				return err
			}
		}

		opt := slog.HandlerOptions{
			AddSource: true,
			Level:     level,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if groups == nil && a.Key == "msg" {
					return slog.Attr{Key: "message", Value: a.Value}
				}
				return a
			},
		}

		var logHandler slog.Handler

		if cfg.Logging.Main.Format == config.MainFormatPlain {
			logHandler = slog.NewTextHandler(out, &opt)
		} else if cfg.Logging.Main.Format == config.MainFormatJson {
			logHandler = slog.NewJSONHandler(out, &opt)
			if cfg.Logging.Main.Request == "auto" {
				cfg.Logging.Main.Request = "true"
			}
		} else {
			return fmt.Errorf(cfg.Error.Messages.InvalidParam, "logging.main.format", cfg.Logging.Main.Format)
		}

		var attr []string

		if cfg.Logging.Main.Request == "true" || cfg.Logging.Main.Request == "1" {
			attr = slices.Concat(attr, []string{
				"uri",
				"path",
				"query",
				"proto",
				"ip",
				"method",
				"host",
				"elapsed",
				"user",
			})
		}

		attr = slices.Concat(attr, cfg.Logging.Main.Headers)

		logHandler = slogContextHandler{logHandler, attr}

		slog.SetDefault(slog.New(logHandler))
	} else {
		slog.SetLogLoggerLevel(10)
	}
	return nil
}

func configureAccessLogging(cfg config.AccessConfig, errorMessages config.ErrorMessages, rootHandler http.Handler) (http.Handler, error) {
	if cfg.Console || len(cfg.Path) > 0 {
		var out io.Writer
		var err error
		if len(cfg.Path) > 0 {
			out, err = makeLogFileWriter(cfg.Path, cfg.Console)

			if err != nil {
				return nil, err
			}
		} else {
			out = os.Stdout
		}

		if cfg.Format == config.AccessFormatCommon {
			rootHandler = handlers.LoggingHandler(out, rootHandler)
		} else if cfg.Format == config.AccessFormatCombined {
			rootHandler = handlers.CombinedLoggingHandler(out, rootHandler)
		} else {
			return nil, fmt.Errorf(errorMessages.InvalidParam, "logging.access.format", cfg.Format)
		}
	}
	return rootHandler, nil
}

type httpContextHandler struct {
	http.Handler
	errCfg config.ErrorConfig
}

func (h httpContextHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqC := pkg.NewRequestContext(req)
	defer func() {
		if err := recover(); err != nil {
			writeErrorMessage(&reqC, w, &h.errCfg, pkg.TypeOfErrorOther, fmt.Sprint(err), "Unexpected Internal Server Error", debug.Stack())
		}
	}()

	h.Handler.ServeHTTP(w, req.WithContext(&reqC))
}

type httpRedirectHandler struct {
	protoAndHost string
}

func (h httpRedirectHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	http.Redirect(w, req, h.protoAndHost+req.RequestURI, http.StatusMovedPermanently)
}

func ListenAndServe(config *config.Config, layerGroup *layer.LayerGroup, auth authentication.Authentication) error {
	if config.Server.Encrypt != nil && config.Server.Encrypt.Domain == "" {
		return fmt.Errorf(config.Error.Messages.ParamRequired, "server.encrypt.domain")
	}

	r := http.ServeMux{}

	if config.Server.Production {
		r.HandleFunc(config.Server.RootPath, handleNoContent)
	} else {
		r.Handle(config.Server.RootPath, &defaultHandler{config, layerGroup, auth})
		// r.HandleFunc("/documentation", defaultHandler)
	}

	tilePath := config.Server.RootPath + config.Server.TilePath + "/{layer}/{z}/{x}/{y}"
	myTileHandler := tileHandler{defaultHandler{config, layerGroup, auth}}

	r.Handle(tilePath, &myTileHandler)
	r.Handle(tilePath+"/", &myTileHandler)

	var rootHandler http.Handler

	rootHandler = &r

	if config.Server.Gzip {
		rootHandler = handlers.CompressHandler(rootHandler)
	}

	rootHandler = httpContextHandler{rootHandler, config.Error}
	rootHandler = http.TimeoutHandler(rootHandler, time.Duration(config.Server.Timeout)*time.Second, config.Error.Messages.Timeout)
	rootHandler, err := configureAccessLogging(config.Logging.Access, config.Error.Messages, rootHandler)

	if err != nil {
		return err
	}

	err = configureMainLogging(config)

	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(pkg.BackgroundContext(), InterruptFlags...)
	defer stop()

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
			httpPort := config.Server.Encrypt.HttpPort
			httpHostPort := net.JoinHostPort(config.Server.BindHost, strconv.Itoa(httpPort))

			if config.Server.Encrypt.Certificate != "" && config.Server.Encrypt.KeyFile != "" {
				if httpPort != 0 {
					go http.ListenAndServe(httpHostPort, httpRedirectHandler{protoAndHost: "https://" + config.Server.Encrypt.Domain})
				}

				srvErr <- srv.ListenAndServeTLS(config.Server.Encrypt.Certificate, config.Server.Encrypt.KeyFile)
			} else {
				//Let's Encrypt workflow
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
					go http.ListenAndServe(httpHostPort, certManager.HTTPHandler(nil))
				}

				srv.TLSConfig = certManager.TLSConfig()

				srvErr <- srv.ListenAndServeTLS("", "")
			}
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
	return err
}
