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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/static"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	_ "github.com/Michad/tilegroxy/internal/analytics"
	_ "github.com/Michad/tilegroxy/internal/authentications"
	_ "github.com/Michad/tilegroxy/internal/caches"
	_ "github.com/Michad/tilegroxy/internal/datastores"
	_ "github.com/Michad/tilegroxy/internal/providers"
	_ "github.com/Michad/tilegroxy/internal/secrets"
)

// How long the previous generation of entities gets to flush and release after a hot reload, once
// in-flight requests have had time to finish.
const entityCloseTimeout = 30 * time.Second

var packageName = static.GetPackage()
var version, ref, buildDate = static.GetVersionInformation()

type tileHandler struct {
	entities           reloadableEntities
	entityMutex        sync.RWMutex // Access to the entities struct above should happen inside this mutex to enable requests to be able to complete without disruption when hot reloading occurs
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

	h.entityMutex.Lock()
	oldEntities := h.entities
	h.entities = newEntities
	h.entityMutex.Unlock()
	slog.WarnContext(pkg.BackgroundContext(), "Completed refreshing entities from configuration")

	// Release the previous generation in the background. Requests that started before the swap hold
	// their own copy of it, so closing has to wait for them to finish; the server timeout is the
	// longest any of them can still be running.
	go h.closeOldEntities(oldEntities)
}

// currentEntities returns the generation currently serving requests. Shutdown closes this rather than the
// generation the server was originally handed, which a reload may already have released
func (h *tileHandler) currentEntities() *entities.Entities {
	h.entityMutex.RLock()
	defer h.entityMutex.RUnlock()

	return h.entities.all
}

func (h *tileHandler) closeOldEntities(old reloadableEntities) {
	if old.all == nil {
		return
	}

	ctx := pkg.BackgroundContext()

	grace := time.Duration(0)
	if old.config != nil {
		grace = time.Duration(old.config.Server.Timeout) * time.Second
	}

	time.Sleep(grace)

	closeCtx, cancel := context.WithTimeout(ctx, entityCloseTimeout)
	defer cancel()

	if err := old.all.Close(closeCtx); err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Error releasing entities from the previous configuration: %v", err))
	} else {
		slog.InfoContext(ctx, "Released entities from the previous configuration")
	}
}

func setServiceSpanAttributes(span trace.Span) {
	if !span.IsRecording() {
		return
	}

	span.SetAttributes(
		attribute.String("service.name", "tilegroxy"),
		attribute.String("service.version", version+"-"+ref),
		attribute.String("service.build", buildDate),
		attribute.String("code.namespace", static.GetPackage()+"/internal/server/tile_handler.go"),
		attribute.String("code.function", "ServeHTTP"),
	)
}

func setTileSpanAttributes(span trace.Span, tileReq pkg.TileRequest) {
	if !span.IsRecording() {
		return
	}

	span.SetAttributes(
		attribute.String("tilegroxy.layer.name", tileReq.LayerName),
		attribute.Int("tilegroxy.coordinate.x", tileReq.X),
		attribute.Int("tilegroxy.coordinate.y", tileReq.Y),
		attribute.Int("tilegroxy.coordinate.z", tileReq.Z),
	)
}

// writeTile sends the rendered tile body, recording the outcome on the span. A write failure
// still means the tile itself was generated successfully, so the caller counts it as a success
// either way. When the request carries a matching If-None-Match the body is skipped in favor of a
// 304, which is likewise a success.
func writeTile(ctx context.Context, w http.ResponseWriter, req *http.Request, span trace.Span, img *pkg.Image) {
	if img.ContentType != "" {
		w.Header().Add("Content-Type", img.ContentType)
	}

	etag := etagFor(img.Content)
	w.Header().Set("ETag", etag)

	if requestETagMatches(req.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		span.SetStatus(codes.Ok, "")

		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(img.Content)))
	w.WriteHeader(http.StatusOK)

	_, err := w.Write(img.Content)

	if err == nil {
		span.SetStatus(codes.Ok, "")
		return
	}

	if errors.Is(err, context.Canceled) || err.Error() == context.Canceled.Error() {
		slog.DebugContext(ctx, "Request canceled during write")
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, "Result write error")
	slog.WarnContext(ctx, fmt.Sprintf("Unable to write to request due to %v", err))
}

func (h *tileHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	span := trace.SpanFromContext(ctx)

	// Make a copy of entities to ensure entire request goes against the same version of entities even if a reload occurs - and avoid wrapping full request execution in lock
	h.entityMutex.RLock()
	entities := h.entities
	h.entityMutex.RUnlock()

	h.tileAllCounter.Add(ctx, 1)

	setServiceSpanAttributes(span)

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

	setTileSpanAttributes(span, tileReq)

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

	writeTile(ctx, w, req, span, img)

	// This isn't in the else clause because the tile was still generated successfully even though request errored
	h.tileSuccessCounter.Add(ctx, 1)

	// Recorded after the response is written so analytics never sits on the latency path. Like the
	// success counter above, a failed write still counts as usage: the tile was produced and, for
	// operators tracking consumption of a paid upstream, the cost was incurred.
	entities.recordAnalytics(ctx, tileReq, img)
}

// recordAnalytics emits a usage event for a successfully served tile. It resolves the layer a
// second time to get its configured ID and skip flag; FindLayer is a cheap in-memory match and
// keeps this off RenderTile, which also runs for seeding, health checks and ref providers.
func (h *reloadableEntities) recordAnalytics(ctx context.Context, tileReq pkg.TileRequest, img *pkg.Image) {
	if h.analytics.Empty() {
		return
	}

	l := h.layerGroup.FindLayer(ctx, tileReq.LayerName)

	if l == nil || l.Config.SkipAnalytics {
		return
	}

	userID := ""
	if u, ok := pkg.UserIDFromContext(ctx); ok && u != nil {
		userID = *u
	}

	event := analytics.Event{
		Time:      time.Now(),
		LayerID:   l.ID,
		LayerName: tileReq.LayerName,
		Z:         tileReq.Z,
		X:         tileReq.X,
		Y:         tileReq.Y,
		UserID:    userID,
	}

	h.analytics.RecordEvent(ctx, event, analytics.FieldSource{
		LayerName:   tileReq.LayerName,
		Bytes:       len(img.Content),
		ContentType: img.ContentType,
	})
}

// etagFor produces a strong ETag from the tile content. Content is already in memory by the time
// this runs, so hashing it is cheap - this is the "cheap win" for HTTP caching semantics: the
// internal cache saves the upstream call, but without this every byte still crosses the wire on
// every request and browsers re-fetch every tile on every pan/zoom.
func etagFor(content []byte) string {
	sum := sha256.Sum256(content)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// requestETagMatches implements the If-None-Match precondition from RFC 9110 §13.1.2: a
// comma-separated list of one or more entity tags, or "*" to match any current representation.
func requestETagMatches(ifNoneMatch string, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if ifNoneMatch == "*" {
		return true
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
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
