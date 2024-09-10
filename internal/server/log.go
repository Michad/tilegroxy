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
	"slices"
	"strings"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/static"
	"github.com/gorilla/handlers"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

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
	if !cfg.Logging.Main.Console && len(cfg.Logging.Main.Path) == 0 {
		slog.SetLogLoggerLevel(slog.LevelError + 1)
		return nil
	}

	var err error
	var out io.Writer
	if len(cfg.Logging.Main.Path) > 0 {
		out, err = makeLogFileWriter(cfg.Logging.Main.Path, cfg.Logging.Main.Console)
		if err != nil {
			return err
		}
	} else {
		out = os.Stdout
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

	switch cfg.Logging.Main.Format {
	case config.MainFormatPlain:
		logHandler = slog.NewTextHandler(out, &opt)
	case config.MainFormatJSON:
		logHandler = slog.NewJSONHandler(out, &opt)
		if cfg.Logging.Main.Request == "auto" {
			cfg.Logging.Main.Request = "true"
		}
	default:
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

	if cfg.Telemetry.Enabled {
		otelHandler := otelslog.NewHandler(static.GetPackage())
		logHandler = MultiHandler{[]slog.Handler{logHandler, otelHandler}}
	}

	slog.SetDefault(slog.New(logHandler))

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

		switch cfg.Format {
		case config.AccessFormatCommon:
			rootHandler = handlers.LoggingHandler(out, rootHandler)
		case config.AccessFormatCombined:
			rootHandler = handlers.CombinedLoggingHandler(out, rootHandler)
		default:
			return nil, fmt.Errorf(errorMessages.InvalidParam, "logging.access.format", cfg.Format)
		}
	}
	return rootHandler, nil
}
