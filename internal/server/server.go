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
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/internal/layers"

	"github.com/gorilla/handlers"
)

func handleNoContent(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

type TypeOfError int

const (
	TypeOfErrorBounds = iota
	TypeOfErrorAuth
	TypeOfErrorProvider
	TypeOfErrorOtherBadRequest
	TypeOfErrorOther
)

func writeError(ctx context.Context, w http.ResponseWriter, cfg *config.ErrorConfig, errorType TypeOfError, message string, args ...any) {
	var status int
	if !cfg.SuppressStatusCode {
		if errorType == TypeOfErrorAuth {
			status = http.StatusUnauthorized
		} else if errorType == TypeOfErrorBounds {
			status = http.StatusBadRequest
		} else if errorType == TypeOfErrorProvider {
			status = http.StatusInternalServerError
		} else if errorType == TypeOfErrorOtherBadRequest {
			status = http.StatusBadRequest
		} else {
			status = http.StatusInternalServerError
		}
	} else {
		status = http.StatusOK
	}

	// fullMessage := fmt.Sprintf(message, args...)
	slog.WarnContext(ctx, message, args...)

	if cfg.Mode == config.ModeErrorPlainText {
		w.WriteHeader(status)
		w.Write([]byte(message))
	} else if cfg.Mode == config.ModeErrorNoError {
		w.WriteHeader(status)
	} else if cfg.Mode == config.ModeErrorImageHeader || cfg.Mode == config.ModeErrorImage {
		if cfg.Mode == config.ModeErrorImageHeader {
			w.Header().Add("x-error-message", message)
		}
		w.WriteHeader(status)

		var imgPath string
		if errorType == TypeOfErrorBounds {
			imgPath = cfg.Images.OutOfBounds
		} else if errorType == TypeOfErrorAuth {
			imgPath = cfg.Images.Authentication
		} else if errorType == TypeOfErrorProvider {
			imgPath = cfg.Images.Provider
		} else {
			imgPath = cfg.Images.Other
		}

		img, err2 := images.GetStaticImage(imgPath)
		if img != nil {
			w.Write(*img)
		}

		if err2 != nil {
			slog.ErrorContext(ctx, err2.Error())
		}
	} else {
		w.WriteHeader(status)
		slog.ErrorContext(ctx, "Invalid error mode! Falling back to none!")
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
func configureMainLogging(cfg *config.Config) error {
	if cfg.Logging.MainLog.EnableStandardOut || len(cfg.Logging.MainLog.Path) > 0 {
		var out io.Writer
		if len(cfg.Logging.MainLog.Path) > 0 {
			logFile, err := os.OpenFile(cfg.Logging.MainLog.Path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)

			if err != nil {
				return err
			}

			if cfg.Logging.MainLog.EnableStandardOut {
				out = io.MultiWriter(os.Stdout, out)
			} else {
				out = logFile
			}
		} else if cfg.Logging.MainLog.EnableStandardOut {
			out = os.Stdout
		} else {
			panic("Impossible logic error")
		}

		var level slog.Level
		err := level.UnmarshalText([]byte(cfg.Logging.MainLog.Level))

		if err != nil {
			return err
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

		if cfg.Logging.MainLog.Format == config.MainLogFormatPlain {
			logHandler = slog.NewTextHandler(out, &opt)
		} else if cfg.Logging.MainLog.Format == config.MainLogFormatJson {
			logHandler = slog.NewJSONHandler(out, &opt)
		} else {
			return fmt.Errorf(cfg.Error.Messages.InvalidParam, "logging.mainlog.format", cfg.Logging.MainLog.Format)
		}
		logHandler = slogContextHandler{logHandler, slices.Concat([]string{"url"}, cfg.Logging.MainLog.IncludeHeaders)}

		slog.SetDefault(slog.New(logHandler))
	} else {
		slog.SetLogLoggerLevel(10)
	}
	return nil
}

func configureAccessLogging(cfg config.AccessLogConfig, errorMessages config.ErrorMessages, rootHandler http.Handler) (http.Handler, error) {
	if cfg.EnableStandardOut || len(cfg.Path) > 0 {
		var out io.Writer
		if len(cfg.Path) > 0 {
			logFile, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)

			if err != nil {
				return nil, err
			}

			if cfg.EnableStandardOut {
				out = io.MultiWriter(os.Stdout, out)
			} else {
				out = logFile
			}
		} else if cfg.EnableStandardOut {
			out = os.Stdout
		} else {
			panic("Impossible logic error")
		}

		if cfg.Format == config.AccessLogFormatCommon {
			rootHandler = handlers.LoggingHandler(out, rootHandler)
		} else if cfg.Format == config.AccessLogFormatCombined {
			rootHandler = handlers.CombinedLoggingHandler(out, rootHandler)
		} else {
			return nil, fmt.Errorf(errorMessages.InvalidParam, "logging.accesslog.format", cfg.Format)
		}
	}
	return rootHandler, nil
}

// Custom context type. Links back to request so we can pull attrs into the structured log
type reqCtx struct {
	context.Context
	req *http.Request
}

func (c *reqCtx) Value(keyAny any) any {
	key, ok := keyAny.(string)
	if !ok {
		return nil
	}

	if key == "url" {
		return c.req.URL.String()
	}

	h := c.req.Header[key]

	if h != nil {
		if len(h) == 1 {
			return h[0]
		}
		return h
	}

	return nil
}

type httpContextHandler struct {
	http.Handler
	errCfg config.ErrorConfig
}

func (h httpContextHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqC := reqCtx{req.Context(), req}
	defer func() {
		if err := recover(); err != nil {
			writeError(&reqC, w, &h.errCfg, TypeOfErrorOther, "Unexpected Internal Server Error", "stack", string(debug.Stack()))
		}
	}()

	h.Handler.ServeHTTP(w, req.WithContext(&reqC))
}

func ListenAndServe(config *config.Config, layerList []*layers.Layer, auth *authentication.Authentication) error {
	r := http.ServeMux{}

	layerMap := make(map[string]*layers.Layer)
	for _, l := range layerList {
		layerMap[l.Id] = l
	}

	if config.Server.Production {
		r.HandleFunc("/", handleNoContent)
	} else {
		r.Handle("/", &defaultHandler{config, layerMap, auth})
		// r.HandleFunc("/documentation", defaultHandler)
	}
	r.Handle(config.Server.ContextRoot+"/{layer}/{z}/{x}/{y}", &tileHandler{defaultHandler{config, layerMap, auth}})

	var rootHandler http.Handler

	rootHandler = &r

	if config.Server.Gzip {
		rootHandler = handlers.CompressHandler(rootHandler)
	}

	rootHandler, err := configureAccessLogging(config.Logging.AccessLog, config.Error.Messages, rootHandler)
	rootHandler = httpContextHandler{rootHandler, config.Error}

	if err != nil {
		return err
	}

	err = configureMainLogging(config)

	if err != nil {
		return err
	}

	slog.InfoContext(context.Background(), "Binding...")

	return http.ListenAndServe(config.Server.BindHost+":"+strconv.Itoa(config.Server.Port), rootHandler)
}
