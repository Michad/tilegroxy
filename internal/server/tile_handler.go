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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/providers"
)

type tileHandler struct {
	defaultHandler
}

func (h *tileHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context().(*internal.RequestContext)
	slog.DebugContext(ctx, "server: tile handler started")
	defer slog.DebugContext(ctx, "server: tile handler ended")

	if !(*h.auth).CheckAuthentication(req, ctx) {
		writeError(ctx, w, &h.config.Error, TypeOfErrorAuth, h.config.Error.Messages.NotAuthorized)
		return
	}

	layerName := req.PathValue("layer")
	zStr := req.PathValue("z")
	xStr := req.PathValue("x")
	yStr := req.PathValue("y")

	z, err := strconv.Atoi(zStr)

	if err != nil {
		writeError(ctx, w, &h.config.Error, TypeOfErrorBounds, fmt.Sprintf(h.config.Error.Messages.InvalidParam, "z", zStr))
		return
	}

	x, err := strconv.Atoi(xStr)

	if err != nil {
		writeError(ctx, w, &h.config.Error, TypeOfErrorBounds, fmt.Sprintf(h.config.Error.Messages.InvalidParam, "x", xStr))
		return
	}

	y, err := strconv.Atoi(yStr)

	if err != nil {
		writeError(ctx, w, &h.config.Error, TypeOfErrorBounds, fmt.Sprintf(h.config.Error.Messages.InvalidParam, "y", yStr))
		return
	}

	tileReq := internal.TileRequest{LayerName: layerName, Z: z, X: x, Y: y}

	_, err = tileReq.GetBounds()

	if err != nil {
		var re internal.RangeError
		if errors.As(err, &re) {
			writeError(ctx, w, &h.config.Error, TypeOfErrorBounds, fmt.Sprintf(h.config.Error.Messages.RangeError, re.ParamName, re.MinValue, re.MaxValue))
		} else {
			writeError(ctx, w, &h.config.Error, TypeOfErrorOther, fmt.Sprintf(h.config.Error.Messages.ServerError, err), "stack", string(debug.Stack()))
		}
		return
	}

	if h.layerMap[layerName] == nil {
		writeError(ctx, w, &h.config.Error, TypeOfErrorOtherBadRequest, fmt.Sprintf(h.config.Error.Messages.InvalidParam, "layer", layerName))
		return
	}

	layer := h.layerMap[layerName]

	img, err := layer.RenderTile(ctx, tileReq)

	if err != nil {
		var ae providers.AuthError
		if errors.As(err, &ae) {
			writeError(ctx, w, &h.config.Error, TypeOfErrorAuth, h.config.Error.Messages.NotAuthorized)
		} else {
			writeError(ctx, w, &h.config.Error, TypeOfErrorOther, fmt.Sprintf(h.config.Error.Messages.ServerError, err), "stack", string(debug.Stack()))
		}

		return
	}

	if img == nil {
		writeError(ctx, w, &h.config.Error, TypeOfErrorProvider, h.config.Error.Messages.ProviderError)
		return
	}

	for h, v := range h.config.Server.Headers {
		w.Header().Add(h, v)
	}

	if !h.config.Server.Production {
		version, _, _ := internal.GetVersionInformation()
		w.Header().Add("X-Powered-By", "tilegroxy "+version)
	}

	w.WriteHeader(http.StatusOK)

	w.Write(*img)
}
