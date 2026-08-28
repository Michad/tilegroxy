// Copyright 2026 Michael Davis
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
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/layer"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type tileJSONIndexEntry struct {
	Name     string `json:"name"`
	TileJSON string `json:"tilejson"`
}

// tileJSONHandler serves both TileJSON endpoints. It tracks entities and generations the same way
// tileHandler does, as a separate instance rather than sharing tileHandler's, since the index and
// per-layer routes are distinct http.Handler registrations.
type tileJSONHandler struct {
	entities    reloadableEntities
	entityMutex sync.RWMutex
	// index selects between the two endpoints sharing this handler: true serves the
	// RootPath/IndexPath listing, false serves TilePath/{layer}.json for one layer.
	index bool
}

func newTileJSONHandler(handler reloadableEntities, index bool) *tileJSONHandler {
	return &tileJSONHandler{entities: handler, index: index}
}

func (h *tileJSONHandler) reloadEntities(newEntities reloadableEntities) {
	h.entityMutex.Lock()
	oldEntities := h.entities
	h.entities = newEntities
	h.entityMutex.Unlock()

	if oldEntities.gen != nil {
		oldEntities.gen.markClosing(pkg.BackgroundContext(), generationCloseFloor)
	}
}

// tileJSONHandlers bundles the two TileJSON endpoints and their routes, letting setupHandlers
// wire them in with a few calls regardless of whether TileJSON is enabled.
type tileJSONHandlers struct {
	index        *tileJSONHandler
	document     *tileJSONHandler
	indexHTTP    http.Handler
	documentHTTP http.Handler
	indexPath    string
	documentPath string
}

// setupTileJSONHandlers builds the TileJSON handlers when enabled. Its zero value (TileJSON
// disabled) is safe to use directly: every method below no-ops on nil handlers.
func setupTileJSONHandlers(cfg *config.Config, reloadable reloadableEntities) *tileJSONHandlers {
	if !cfg.Server.TileJSON.Enabled {
		return &tileJSONHandlers{}
	}

	index := newTileJSONHandler(reloadable, true)
	document := newTileJSONHandler(reloadable, false)

	return &tileJSONHandlers{
		index:        index,
		document:     document,
		indexHTTP:    index,
		documentHTTP: document,
		indexPath:    cfg.Server.RootPath + cfg.Server.TileJSON.IndexPath,
		documentPath: cfg.Server.RootPath + cfg.Server.TilePath + "/{layerjson}",
	}
}

func (t *tileJSONHandlers) reloadEntities(cfg *config.Config, ent *entities.Entities, gen *generation) {
	if t.index == nil {
		return
	}

	t.index.reloadEntities(newReloadableEntities(cfg, ent, gen))
	t.document.reloadEntities(newReloadableEntities(cfg, ent, gen))
}

func (t *tileJSONHandlers) wrapWithTelemetry() {
	if t.index == nil {
		return
	}

	t.indexHTTP = otelhttp.NewHandler(t.indexHTTP, t.indexPath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))
	t.documentHTTP = otelhttp.NewHandler(t.documentHTTP, t.documentPath, otelhttp.WithMessageEvents(otelhttp.WriteEvents))
}

func (t *tileJSONHandlers) registerRoutes(r *http.ServeMux) {
	if t.index == nil {
		return
	}

	r.Handle(t.indexPath, t.indexHTTP)
	r.Handle(t.documentPath, t.documentHTTP)
}

// publicURLParts is the scheme, host, and path prefix TileJSON URLs are built from.
type publicURLParts struct {
	scheme string
	host   string
	prefix string
}

// parsePublicURL splits a single configured base URL into its scheme, host, and path prefix.
func parsePublicURL(baseURL string) publicURLParts {
	trimmed := strings.TrimSuffix(baseURL, "/")
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		scheme := trimmed[:idx]
		rest := trimmed[idx+3:]
		if slash := strings.Index(rest, "/"); slash >= 0 {
			return publicURLParts{scheme: scheme, host: rest[:slash], prefix: rest[slash:]}
		}
		return publicURLParts{scheme: scheme, host: rest, prefix: ""}
	}
	return publicURLParts{scheme: "https", host: trimmed}
}

// resolvePublicURLs determines the scheme/host/path-prefix(es) tiles are reachable at from the
// caller's perspective. BaseURLs, when configured, always wins, producing one entry per
// configured URL; otherwise it's read from the standard reverse-proxy forwarding headers,
// falling back to the request's own scheme and Host.
func resolvePublicURLs(req *http.Request, baseURLs []string) []publicURLParts {
	if len(baseURLs) > 0 {
		parts := make([]publicURLParts, len(baseURLs))
		for i, baseURL := range baseURLs {
			parts[i] = parsePublicURL(baseURL)
		}
		return parts
	}

	scheme := req.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if req.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := req.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = req.Host
	}

	prefix := strings.TrimSuffix(req.Header.Get("X-Forwarded-Prefix"), "/")

	return []publicURLParts{{scheme: scheme, host: host, prefix: prefix}}
}

func (p publicURLParts) build(path string) string {
	return p.scheme + "://" + p.host + p.prefix + path
}

// layerRestriction reads the auth-populated layer scope from context, mirroring the same
// accessors layer.LayerGroup.checkPermission uses to gate tile requests.
func layerRestriction(ctx context.Context) (bool, []string) {
	ctxLimitLayers, ok := pkg.LimitLayersFromContext(ctx)
	limited := ok && ctxLimitLayers != nil && *ctxLimitLayers

	var allowed []string
	ctxAllowedLayers, ok := pkg.AllowedLayersFromContext(ctx)
	if ok && ctxAllowedLayers != nil {
		allowed = *ctxAllowedLayers
	}

	return limited, allowed
}

// areaRestriction reads the auth-populated geographic restriction from context. Returns nil when
// the caller isn't restricted to a specific area.
func areaRestriction(ctx context.Context) *pkg.Bounds {
	ctxAllowedArea, ok := pkg.AllowedAreaFromContext(ctx)
	if !ok || ctxAllowedArea == nil || ctxAllowedArea.IsNullIsland() {
		return nil
	}

	return ctxAllowedArea
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	data, err := json.Marshal(body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func (h *tileJSONHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	slog.DebugContext(ctx, "server: tilejson handler started")
	defer slog.DebugContext(ctx, "server: tilejson handler ended")

	// Copy the entities and take a hold on their generation in the same critical section as the
	// pointer read, so a concurrent reload either sees this request or hands us the new generation
	h.entityMutex.RLock()
	entities := h.entities
	if entities.gen != nil {
		entities.gen.acquire()
		defer entities.gen.release()
	}
	h.entityMutex.RUnlock()

	entities.writeHeaders(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if !entities.auth.CheckAuthentication(ctx, req) {
		writeError(ctx, w, &entities.config.Error, pkg.UnauthorizedError{Message: "CheckAuthentication returned false"}, config.DataTypeUnknown)
		return
	}

	limitLayers, allowed := layerRestriction(ctx)
	allowedArea := areaRestriction(ctx)

	publicURLs := resolvePublicURLs(req, entities.config.Server.TileJSON.BaseURLs)
	tilePathPrefix := entities.config.Server.RootPath + entities.config.Server.TilePath

	if h.index {
		serveIndex(w, entities, publicURLs[0], tilePathPrefix, limitLayers, allowed)
		return
	}

	serveDocument(ctx, w, req, entities, publicURLs, tilePathPrefix, limitLayers, allowed, allowedArea)
}

func serveIndex(w http.ResponseWriter, entities reloadableEntities, publicURL publicURLParts, tilePathPrefix string, limitLayers bool, allowed []string) {
	entries := make([]tileJSONIndexEntry, 0)

	for _, l := range entities.layerGroup.Layers() {
		if !l.TileJSONEligible() {
			continue
		}

		for _, name := range l.TileJSONNames() {
			if limitLayers && !layerNameAllowed(name, l.ID, allowed) {
				continue
			}

			entries = append(entries, tileJSONIndexEntry{
				Name:     name,
				TileJSON: publicURL.build(tilePathPrefix + "/" + name + ".json"),
			})
		}
	}

	writeJSON(w, http.StatusOK, entries)
}

func serveDocument(ctx context.Context, w http.ResponseWriter, req *http.Request, entities reloadableEntities, publicURLs []publicURLParts, tilePathPrefix string, limitLayers bool, allowed []string, allowedArea *pkg.Bounds) {
	pathValue := req.PathValue("layerjson")
	name, ok := strings.CutSuffix(pathValue, ".json")
	if !ok {
		writeError(ctx, w, &entities.config.Error, pkg.UnauthorizedError{Message: "Layer " + pathValue + " does not exist"}, config.DataTypeUnknown)
		return
	}

	l, foundName := findTileJSONLayer(entities.layerGroup, name)
	if l == nil {
		writeError(ctx, w, &entities.config.Error, pkg.UnauthorizedError{Message: "Layer " + name + " does not exist"}, config.DataTypeUnknown)
		return
	}

	if limitLayers && !layerNameAllowed(foundName, l.ID, allowed) {
		writeError(ctx, w, &entities.config.Error, pkg.UnauthorizedError{Message: "Denying access to non-allowed layer"}, config.DataTypeUnknown)
		return
	}

	tilesURLs := make([]string, len(publicURLs))
	for i, publicURL := range publicURLs {
		tilesURLs[i] = publicURL.build(tilePathPrefix + "/" + foundName + "/{z}/{x}/{y}")
	}
	doc := l.BuildTileJSON(foundName, tilesURLs, allowedArea)

	writeJSON(w, http.StatusOK, doc)
}

// findTileJSONLayer finds the eligible layer whose ID or example list produces the given name,
// returning the layer and the exact name it matched under.
func findTileJSONLayer(lg *layer.LayerGroup, name string) (*layer.Layer, string) {
	for _, l := range lg.Layers() {
		if !l.TileJSONEligible() {
			continue
		}

		for _, candidate := range l.TileJSONNames() {
			if candidate == name {
				return l, candidate
			}
		}
	}

	return nil, ""
}

// layerNameAllowed reports whether either the requested name or the layer's own ID appears in the
// caller's allowed-layers list. A pattern layer's examples aren't themselves configured layer IDs,
// so scope checks fall back to the layer's ID the way tile requests already do via MatchesName.
func layerNameAllowed(name string, layerID string, allowed []string) bool {
	for _, a := range allowed {
		if a == name || a == layerID {
			return true
		}
	}
	return false
}
