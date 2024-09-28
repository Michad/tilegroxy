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
	"net/http"
	"runtime/debug"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

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

	if cfg.AlwaysOK {
		status = http.StatusOK
	}

	return status, level, imgPath
}

func writeError(ctx context.Context, w http.ResponseWriter, cfg *config.ErrorConfig, err error) {
	var te pkg.TypedError
	if errors.As(err, &te) {
		writeErrorMessage(ctx, w, cfg, te.Type(), te.Error(), te.External(cfg.Messages), debug.Stack())
	} else {
		writeErrorMessage(ctx, w, cfg, pkg.TypeOfErrorOther, err.Error(), fmt.Sprintf(cfg.Messages.ServerError, err), debug.Stack())
	}
}

func writeErrorMessage(ctx context.Context, w http.ResponseWriter, cfg *config.ErrorConfig, errorType pkg.TypeOfError, internalMessage string, externalMessage string, stack []byte) {
	status, level, imgPath := errorVars(cfg, errorType)

	slog.Log(ctx, level, internalMessage, "stack", string(stack))

	switch cfg.Mode {
	case config.ModeErrorPlainText:
		w.WriteHeader(status)
		_, err := w.Write([]byte(externalMessage))
		if err != nil {
			slog.WarnContext(ctx, fmt.Sprintf("error writing error %v", err))
		}
	case config.ModeErrorImage, config.ModeErrorImageHeader:
		if cfg.Mode == config.ModeErrorImageHeader {
			w.Header().Add("X-Error-Message", externalMessage)
		}
		w.WriteHeader(status)

		img, err2 := images.GetStaticImage(imgPath)
		if img != nil && err2 == nil {
			_, err2 = w.Write(*img)
		}

		if err2 != nil {
			slog.ErrorContext(ctx, err2.Error())
		}
	default:
		w.WriteHeader(status)
	}
}
