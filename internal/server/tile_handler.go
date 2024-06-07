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
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Michad/tilegroxy/pkg"
)

type tileHandler struct {
	defaultHandler
}

func (h *tileHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// ctx := req.Context()
	slog.Debug("server: tile handler started")
	defer slog.Debug("server: tile handler ended")

	if !(*h.auth).Preauth(req) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	layerName := req.PathValue("layer")
	zStr := req.PathValue("z")
	xStr := req.PathValue("x")
	yStr := req.PathValue("y")

	z, err := strconv.Atoi(zStr)

	if err != nil {
		writeError(w, &h.config.Error, http.StatusBadRequest, h.config.Error.Messages.InvalidParam, "z", zStr)
		return
	}

	x, err := strconv.Atoi(xStr)

	if err != nil {
		writeError(w, &h.config.Error, http.StatusBadRequest, h.config.Error.Messages.InvalidParam, "x", xStr)
		return
	}

	y, err := strconv.Atoi(yStr)

	if err != nil {
		writeError(w, &h.config.Error, http.StatusBadRequest, h.config.Error.Messages.InvalidParam, "y", yStr)
		return
	}

	tileReq := pkg.TileRequest{LayerName: layerName, Z: z, X: x, Y: y}

	_, err = tileReq.GetBounds()

	if err != nil {
		var re pkg.RangeError
		if errors.As(err, &re) {
			writeError(w, &h.config.Error, http.StatusBadRequest, h.config.Error.Messages.RangeError, re.ParamName, re.MinValue, re.MaxValue)
		} else {
			writeError(w, &h.config.Error, http.StatusInternalServerError, h.config.Error.Messages.ServerError, err)
		}
		return
	}

	if h.layerMap[layerName] == nil {
		writeError(w, &h.config.Error, http.StatusBadRequest, h.config.Error.Messages.InvalidParam, "layer", layerName)
		return
	}

	layer := h.layerMap[layerName]

	img, err := layer.RenderTile(tileReq)

	if err != nil {
		writeError(w, &h.config.Error, http.StatusInternalServerError, h.config.Error.Messages.ServerError, err)
		return
	}

	if img == nil {
		writeError(w, &h.config.Error, http.StatusInternalServerError, h.config.Error.Messages.ProviderError)
		return
	}

	w.WriteHeader(http.StatusOK)

	for h, v := range h.config.Server.StaticHeaders {
		w.Header().Add(h, v)
	}

	w.Write(*img)
}
