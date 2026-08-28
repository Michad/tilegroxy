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
	"html/template"
	"log/slog"
	"net/http"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

// previewHandler serves a developer-only HTML page embedding a Leaflet map for one layer, so an
// operator can visually check what a layer renders without a separate GIS client. It tracks
// entities and generations the same way tileJSONHandler does, as its own instance rather than
// sharing tileHandler's, since it's a distinct http.Handler registration.
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

// previewTemplateData is what previewPageTemplate renders. Fields are exported only because
// html/template requires it; this type isn't used outside this file.
type previewTemplateData struct {
	LayerName   string
	TileURL     template.JS
	HasBounds   bool
	South       float64
	North       float64
	West        float64
	East        float64
	MinZoom     int
	MaxZoom     int
	IsVector    bool
	Attribution string
}

// previewPageTemplate renders the standalone HTML page. Leaflet is loaded from its own CDN with
// Subresource Integrity hashes pinned to a specific release, rather than embedded, since it's a
// third-party asset unrelated to tilegroxy's own generated documentation site (which is what
// internal/website embeds).
var previewPageTemplate = template.Must(template.New("preview").Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tilegroxy preview - {{.LayerName}}</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css"
  integrity="sha256-p4NxAoJBhIIN+hmNHrzRCf9tD/miZyoHS5obTRR9BMY=" crossorigin="">
<style>
  html, body, #map { height: 100%; margin: 0; }
  #vector-notice {
    position: absolute; z-index: 1000; top: 10px; left: 50px;
    background: white; padding: 6px 10px; border-radius: 4px;
    box-shadow: 0 1px 5px rgba(0,0,0,0.4); font: 12px sans-serif; max-width: 60%;
  }
</style>
</head>
<body>
{{if .IsVector}}
<!-- This layer serves vector tiles (MVT). Leaflet has no built-in vector renderer, so this
     preview can only confirm tiles are reachable, not draw them. Requesting a tile URL directly
     in the browser is the reliable way to inspect vector tile content. -->
<div id="vector-notice">This is a vector tile layer. Leaflet cannot render vector tiles directly, so the map below will appear blank even when tiles load successfully.</div>
{{end}}
<div id="map"></div>
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"
  integrity="sha256-20nQCchB9co0qIjJZRGuk2/Z9VM+kNiyxNV1lvTlZBo=" crossorigin=""></script>
<script>
  var map = L.map('map');

  {{if .HasBounds}}
  map.fitBounds([[{{.South}}, {{.West}}], [{{.North}}, {{.East}}]]);
  {{else}}
  map.setView([0, 0], {{.MinZoom}});
  {{end}}

  L.tileLayer("{{.TileURL}}", {
    minZoom: {{.MinZoom}},
    maxZoom: {{.MaxZoom}},
    attribution: {{.Attribution}}
  }).addTo(map);
</script>
</body>
</html>
`))

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
		// Mirrors tileJSONHandler's unknown-layer response: a generic auth-shaped error rather
		// than a distinctive 404, so an unauthenticated caller can't use this to enumerate
		// configured layer names.
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

	minZoom := 0
	if l.Config.MinZoom != nil {
		minZoom = *l.Config.MinZoom
	}

	maxZoom := pkg.MaxZoom
	if l.Config.MaxZoom != nil {
		maxZoom = *l.Config.MaxZoom
	}

	// The map only centers on the layer's own configured bounds. Falling back to a world default
	// when unset avoids fitBounds collapsing to a single, unhelpfully zoomed-in point.
	hasBounds := l.Config.Bounds != (config.BoundsConfig{})
	bounds := pkg.WorldBounds()
	if hasBounds {
		bounds = pkg.Bounds{
			South: l.Config.Bounds.South,
			North: l.Config.Bounds.North,
			West:  l.Config.Bounds.West,
			East:  l.Config.Bounds.East,
			SRID:  pkg.SRIDWGS84,
		}
	}

	if allowedArea := areaRestriction(ctx); allowedArea != nil {
		bounds = bounds.IntersectionWith(*allowedArea)
		hasBounds = true
	}

	data := previewTemplateData{
		LayerName:   name,
		TileURL:     template.JS(tileURL), //nolint:gosec // tileURL is built from configured/forwarded host parts, not raw user input, and is placed inside a JS string literal for L.tileLayer
		HasBounds:   hasBounds,
		South:       bounds.South,
		North:       bounds.North,
		West:        bounds.West,
		East:        bounds.East,
		MinZoom:     minZoom,
		MaxZoom:     maxZoom,
		IsVector:    l.DataType == config.DataTypeMVT,
		Attribution: l.Config.Attribution,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := previewPageTemplate.Execute(w, data); err != nil {
		slog.WarnContext(ctx, "Unable to write to preview request due to "+err.Error())
	}
}
