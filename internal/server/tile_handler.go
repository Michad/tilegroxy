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
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/static"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	_ "github.com/Michad/tilegroxy/internal/authentications"
	_ "github.com/Michad/tilegroxy/internal/caches"
	_ "github.com/Michad/tilegroxy/internal/providers"
	_ "github.com/Michad/tilegroxy/internal/secrets"
)

const name = "github.com/michad/tilegroxy"

var (
	tracer         = otel.Tracer(name)
	meter          = otel.Meter(name)
	logger         = otelslog.NewLogger(name)
	tileCounter, _ = meter.Int64Counter("tiles", metric.WithUnit("requests"), metric.WithDescription("Number of total tile requests "))
)

type tileHandler struct {
	defaultHandler
}

func (h *tileHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx, span := tracer.Start(req.Context(), "tile")
	defer span.End()

	slog.DebugContext(ctx, "server: tile handler started")
	defer slog.DebugContext(ctx, "server: tile handler ended")

	if !h.auth.CheckAuthentication(req, ctx) {
		writeError(ctx, w, &h.config.Error, pkg.UnauthorizedError{Message: "CheckAuthentication returned false"})
		return
	}

	layerName := req.PathValue("layer")
	zStr := req.PathValue("z")
	xStr := req.PathValue("x")
	yStr := req.PathValue("y")

	z, err := strconv.Atoi(zStr)

	if err != nil {
		writeError(ctx, w, &h.config.Error, pkg.InvalidArgumentError{Name: "z", Value: zStr})
		return
	}

	x, err := strconv.Atoi(xStr)

	if err != nil {
		writeError(ctx, w, &h.config.Error, pkg.InvalidArgumentError{Name: "x", Value: xStr})
		return
	}

	y, err := strconv.Atoi(yStr)

	if err != nil {
		writeError(ctx, w, &h.config.Error, pkg.InvalidArgumentError{Name: "y", Value: yStr})
		return
	}

	tileReq := pkg.TileRequest{LayerName: layerName, Z: z, X: x, Y: y}

	_, err = tileReq.GetBounds()

	if err != nil {
		writeError(ctx, w, &h.config.Error, err)
		return
	}

	img, err := h.layerGroup.RenderTile(ctx, tileReq)

	if err != nil {
		writeError(ctx, w, &h.config.Error, err)
		return
	}

	if img == nil {
		writeErrorMessage(ctx, w, &h.config.Error, pkg.TypeOfErrorProvider, "Tile rendered as nil but no error returned", h.config.Error.Messages.ProviderError, nil)
		return
	}

	for h, v := range h.config.Server.Headers {
		w.Header().Add(h, v)
	}

	if !h.config.Server.Production {
		version, _, _ := static.GetVersionInformation()
		w.Header().Add("X-Powered-By", "tilegroxy "+version)
	}

	w.WriteHeader(http.StatusOK)

	_, err = w.Write(*img)

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Unable to write to request due to %v", err))
	}

	tileCounter.Add(ctx, 1)
}
