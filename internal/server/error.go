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

const StatusClientClosed = 499

func errorVars(cfg *config.ErrorConfig, errorType pkg.TypeOfError, dataType config.DataType) (int, slog.Level, string, string) {
	var status int
	var level slog.Level
	var imgPath string
	var contentType string

	switch errorType {
	case pkg.TypeOfErrorAuth:
		level = slog.LevelDebug
		status = http.StatusUnauthorized
		// Always PNG: picking the vector variant would leak the layer's data type to a caller
		// who hasn't been authenticated yet.
		imgPath, contentType = cfg.Images.Authentication, "image/png"
	case pkg.TypeOfErrorBounds:
		level = slog.LevelDebug
		status = http.StatusBadRequest
		imgPath, contentType = errorImage(cfg.Images.OutOfBounds, cfg.Images.OutOfBoundsMvt, dataType)
	case pkg.TypeOfErrorProvider:
		level = slog.LevelInfo
		status = http.StatusInternalServerError
		imgPath, contentType = errorImage(cfg.Images.Provider, cfg.Images.ProviderMvt, dataType)
	case pkg.TypeOfErrorBadRequest:
		level = slog.LevelDebug
		status = http.StatusBadRequest
		imgPath, contentType = errorImage(cfg.Images.Other, cfg.Images.OtherMvt, dataType)
	case pkg.TypeOfErrorTimeout:
		level = slog.LevelWarn
		status = http.StatusServiceUnavailable
		imgPath, contentType = errorImage(cfg.Images.Other, cfg.Images.OtherMvt, dataType)
	default:
		level = slog.LevelWarn
		status = http.StatusInternalServerError
		imgPath, contentType = errorImage(cfg.Images.Other, cfg.Images.OtherMvt, dataType)
	}

	if cfg.AlwaysOK {
		status = http.StatusOK
	}

	return status, level, imgPath, contentType
}

// errorImage picks the raster or vector error image, and its Content-Type, based on the
// erroring layer's data type. A layer whose data type couldn't be determined keeps the
// long-standing PNG behavior.
func errorImage(rasterPath string, mvtPath string, dataType config.DataType) (string, string) {
	if dataType == config.DataTypeMVT {
		return mvtPath, images.MvtContentType
	}

	return rasterPath, "image/png"
}

func writeError(ctx context.Context, w http.ResponseWriter, cfg *config.ErrorConfig, err error, dataType config.DataType) {
	var te pkg.TypedError
	if errors.As(err, &te) {
		writeErrorMessage(ctx, w, cfg, te.Type(), te.Error(), te.External(cfg.Messages), debug.Stack(), dataType)
	} else if errors.Is(err, context.Canceled) || err.Error() == context.Canceled.Error() {
		slog.DebugContext(ctx, err.Error())
		w.WriteHeader(StatusClientClosed)
	} else {
		writeErrorMessage(ctx, w, cfg, pkg.TypeOfErrorOther, err.Error(), fmt.Sprintf(cfg.Messages.ServerError, err), debug.Stack(), dataType)
	}
}

func writeErrorMessage(ctx context.Context, w http.ResponseWriter, cfg *config.ErrorConfig, errorType pkg.TypeOfError, internalMessage string, externalMessage string, stack []byte, dataType config.DataType) {
	status, level, imgPath, contentType := errorVars(cfg, errorType, dataType)

	slog.Log(ctx, level, internalMessage, "stack", string(stack))

	// Nothing else marks an error response as uncacheable - with AlwaysOK the status is even 200 -
	// so a CDN in front of tilegroxy would hold onto "tile unavailable" long after the upstream
	// recovers.
	w.Header().Set("Cache-Control", "no-store")

	switch cfg.Mode {
	case config.ModeErrorPlainText:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, err := w.Write([]byte(externalMessage))
		if err != nil {
			slog.WarnContext(ctx, fmt.Sprintf("error writing error %v", err))
		}
	case config.ModeErrorImage, config.ModeErrorImageHeader:
		if cfg.Mode == config.ModeErrorImageHeader {
			w.Header().Add("X-Error-Message", externalMessage)
		}

		img, err2 := images.GetStaticImage(imgPath)

		if img != nil && err2 == nil {
			w.Header().Set("Content-Type", contentType)
		}

		w.WriteHeader(status)

		if img != nil && err2 == nil {
			_, err2 = w.Write(*img)
		}

		if err2 != nil {
			if errors.Is(err2, context.Canceled) || err2.Error() == context.Canceled.Error() {
				slog.DebugContext(ctx, err2.Error())
			} else {
				slog.ErrorContext(ctx, err2.Error())
			}
		}
	default:
		w.WriteHeader(status)
	}
}
