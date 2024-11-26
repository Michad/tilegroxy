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
	"strconv"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/static"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	_ "github.com/Michad/tilegroxy/internal/authentications"
	_ "github.com/Michad/tilegroxy/internal/caches"
	_ "github.com/Michad/tilegroxy/internal/datastores"
	_ "github.com/Michad/tilegroxy/internal/providers"
	_ "github.com/Michad/tilegroxy/internal/secrets"
)

var packageName = static.GetPackage()
var version, ref, buildDate = static.GetVersionInformation()

type tileHandler struct {
	entities           reloadableEntities
	mux                sync.RWMutex
	tracer             trace.Tracer
	meter              metric.Meter
	tileAllCounter     metric.Int64Counter
	tileValidCounter   metric.Int64Counter
	tileErrorCounter   metric.Int64Counter
	tileSuccessCounter metric.Int64Counter
}

func newTileHandler(handler reloadableEntities) (tileHandler, error) {
	meter := otel.Meter(packageName)

	tileAllCounter, err1 := meter.Int64Counter("tilegroxy.tiles.total.request", metric.WithDescription("Number of total tile requests"))
	tileValidCounter, err2 := meter.Int64Counter("tilegroxy.tiles.total.valid", metric.WithDescription("Number of valid tile requests"))
	tileErrorCounter, err3 := meter.Int64Counter("tilegroxy.tiles.total.error", metric.WithDescription("Number of tile requests that error during generation"))
	tileSuccessCounter, err4 := meter.Int64Counter("tilegroxy.tiles.total.success", metric.WithDescription("Number of tile requests that result in a tile"))

	return tileHandler{
		handler,
		sync.RWMutex{},
		otel.Tracer(packageName),
		meter,
		tileAllCounter,
		tileValidCounter,
		tileErrorCounter,
		tileSuccessCounter,
	}, errors.Join(err1, err2, err3, err4)
}

func (h *tileHandler) reloadEntities(newEntities reloadableEntities) {
	slog.WarnContext(pkg.BackgroundContext(), "Requesting to refresh entities from configuration")
	h.mux.Lock()
	h.entities = newEntities
	h.mux.Unlock()
	slog.WarnContext(pkg.BackgroundContext(), "Completed refreshing entities from configuration")
}

func (h *tileHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	span := trace.SpanFromContext(ctx)
	h.mux.RLock()
	entities := h.entities
	h.mux.RUnlock()

	h.tileAllCounter.Add(ctx, 1)

	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("service.name", "tilegroxy"),
			attribute.String("service.version", version+"-"+ref),
			attribute.String("service.build", buildDate),
			attribute.String("code.namespace", static.GetPackage()+"/internal/server/tile_handler.go"),
			attribute.String("code.function", "ServeHTTP"),
		)
	}

	slog.DebugContext(ctx, "server: tile handler started")
	defer slog.DebugContext(ctx, "server: tile handler ended")

	entities.writeHeaders(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !entities.auth.CheckAuthentication(ctx, req) {
		writeError(ctx, w, &entities.config.Error, pkg.UnauthorizedError{Message: "CheckAuthentication returned false"})
		return
	}

	tileReq, ok := entities.extractAndValidateRequest(ctx, req, span, w)
	if !ok {
		return // We already handled the error in the function
	}

	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("tilegroxy.layer.name", tileReq.LayerName),
			attribute.Int("tilegroxy.coordinate.x", tileReq.X),
			attribute.Int("tilegroxy.coordinate.y", tileReq.Y),
			attribute.Int("tilegroxy.coordinate.z", tileReq.Z),
		)
	}

	_, err := tileReq.GetBounds()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Bad Request")
		writeError(ctx, w, &entities.config.Error, err)
		return
	}

	h.tileValidCounter.Add(ctx, 1)

	img, err := entities.layerGroup.RenderTile(ctx, tileReq)

	if err != nil {
		h.tileErrorCounter.Add(ctx, 1)
		span.RecordError(err)
		span.SetStatus(codes.Error, "Rendering error")
		writeError(ctx, w, &entities.config.Error, err)
		return
	}

	if img == nil {
		h.tileErrorCounter.Add(ctx, 1)
		span.SetStatus(codes.Error, "No result")
		writeErrorMessage(ctx, w, &entities.config.Error, pkg.TypeOfErrorProvider, "Tile rendered as nil but no error returned", entities.config.Error.Messages.ProviderError, nil)
		return
	}

	if img.ContentType != "" {
		w.Header().Add("Content-Type", img.ContentType)
	}

	w.WriteHeader(http.StatusOK)

	_, err = w.Write(img.Content)

	if err != nil {
		if errors.Is(err, context.Canceled) || err.Error() == context.Canceled.Error() {
			slog.DebugContext(ctx, "Request canceled during write")
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.RecordError(err)
			span.SetStatus(codes.Error, "Result write error")
			slog.WarnContext(ctx, fmt.Sprintf("Unable to write to request due to %v", err))
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}

	// This isn't in the else clause because the tile was still generated successfully even though request errored
	h.tileSuccessCounter.Add(ctx, 1)
}

func (h *reloadableEntities) writeHeaders(w http.ResponseWriter) {
	for h, v := range h.config.Server.Headers {
		w.Header().Add(h, v)
	}

	if !h.config.Server.Production {
		w.Header().Add("X-Powered-By", "tilegroxy "+version)
	}
}

func (h *reloadableEntities) extractAndValidateRequest(ctx context.Context, req *http.Request, span trace.Span, w http.ResponseWriter) (pkg.TileRequest, bool) {
	layerName := req.PathValue("layer")
	zStr := req.PathValue("z")
	xStr := req.PathValue("x")
	yStr := req.PathValue("y")

	z, err := strconv.Atoi(zStr)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Bad Request")
		writeError(ctx, w, &h.config.Error, pkg.InvalidArgumentError{Name: "z", Value: zStr})
		return pkg.TileRequest{}, false
	}

	x, err := strconv.Atoi(xStr)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Bad Request")
		writeError(ctx, w, &h.config.Error, pkg.InvalidArgumentError{Name: "x", Value: xStr})
		return pkg.TileRequest{}, false
	}

	y, err := strconv.Atoi(yStr)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Bad Request")
		writeError(ctx, w, &h.config.Error, pkg.InvalidArgumentError{Name: "y", Value: yStr})
		return pkg.TileRequest{}, false
	}

	tileReq := pkg.TileRequest{LayerName: layerName, Z: z, X: x, Y: y}
	return tileReq, true
}
