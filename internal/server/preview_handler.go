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
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type previewHandler struct {
	entities    reloadableEntities
	entityMutex sync.RWMutex
}

func newPreviewHandler(handler reloadableEntities) *previewHandler {
	return &previewHandler{entities: handler}
}

func (h *previewHandler) reloadEntities(newEntities reloadableEntities) {
	h.entityMutex.Lock()
	oldEntities := h.entities
	h.entities = newEntities
	h.entityMutex.Unlock()

	if oldEntities.gen != nil {
		oldEntities.gen.markClosing(pkg.BackgroundContext(), generationCloseFloor)
	}
}

// previewTemplateData is what previewPageTemplate renders. Fields are exported only because html/template requires it
type previewTemplateData struct {
	LayerName         string
	TileURL           string
	HasBounds         bool
	South             float64
	North             float64
	West              float64
	East              float64
	MinZoom           int
	MaxZoom           int
	IsVector          bool
	VectorSourceLayer string
	SourceLayerForced bool
	Attribution       string
}

//go:embed preview_handler.html.tmpl
var previewPageHTML string

var previewPageTemplate = template.Must(template.New("preview").Parse(previewPageHTML))

func previewZoomRange(cfg config.LayerConfig) (int, int) {
	minZoom := 0
	if cfg.MinZoom != nil {
		minZoom = *cfg.MinZoom
	}

	maxZoom := pkg.MaxZoom
	if cfg.MaxZoom != nil {
		maxZoom = *cfg.MaxZoom
	}

	return minZoom, maxZoom
}

func previewBounds(ctx context.Context, cfg config.LayerConfig) (bool, pkg.Bounds) {
	hasBounds := cfg.Bounds != (config.BoundsConfig{})
	bounds := pkg.WorldBounds()
	if hasBounds {
		bounds = pkg.Bounds{
			South: cfg.Bounds.South,
			North: cfg.Bounds.North,
			West:  cfg.Bounds.West,
			East:  cfg.Bounds.East,
			SRID:  pkg.SRIDWGS84,
		}
	}

	if allowedArea := areaRestriction(ctx); allowedArea != nil {
		bounds = bounds.IntersectionWith(*allowedArea)
		hasBounds = true
	}

	return hasBounds, bounds
}

func (h *previewHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	slog.DebugContext(ctx, "server: preview handler started")
	defer slog.DebugContext(ctx, "server: preview handler ended")

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

	name := req.PathValue("layer")

	l := entities.layerGroup.FindLayer(ctx, name)
	if l == nil {
		writeError(ctx, w, &entities.config.Error, pkg.UnauthorizedError{Message: "Layer " + name + " does not exist"}, config.DataTypeUnknown)
		return
	}

	limitLayers, allowed := layerRestriction(ctx)
	if limitLayers && !layerNameAllowed(name, l.ID, allowed) {
		writeError(ctx, w, &entities.config.Error, pkg.UnauthorizedError{Message: "Denying access to non-allowed layer"}, config.DataTypeUnknown)
		return
	}

	publicURL := resolvePublicURLs(req, entities.config.Server.TileJSON.BaseURLs)[0]
	tilePathPrefix := entities.config.Server.RootPath + entities.config.Server.TilePath
	tileURL := publicURL.build(tilePathPrefix + "/" + name + "/{z}/{x}/{y}")

	minZoom, maxZoom := previewZoomRange(l.Config)
	hasBounds, bounds := previewBounds(ctx, l.Config)

	// Best-effort guess at the MVT source-layer name inside the tile, matching the postgis_mvt
	// Provider's own default of falling back to the layer's name. The page probes a sample tile
	// to check this guess and falls back further on its own, unless the operator overrides it
	// with ?name=, in which case we trust that value outright and skip the client-side probe.
	sourceLayer := name
	sourceLayerForced := false
	if override := req.URL.Query().Get("name"); override != "" {
		sourceLayer = override
		sourceLayerForced = true
	}

	data := previewTemplateData{
		LayerName:         name,
		TileURL:           tileURL,
		HasBounds:         hasBounds,
		South:             bounds.South,
		North:             bounds.North,
		West:              bounds.West,
		East:              bounds.East,
		MinZoom:           minZoom,
		MaxZoom:           maxZoom,
		IsVector:          l.DataType == config.DataTypeMVT,
		VectorSourceLayer: sourceLayer,
		SourceLayerForced: sourceLayerForced,
		Attribution:       l.Config.Attribution,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := previewPageTemplate.Execute(w, data); err != nil {
		slog.WarnContext(ctx, "Unable to write to preview request due to "+err.Error())
	}
}
