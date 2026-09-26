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
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type previewHandler struct {
	generationHolder
}

func newPreviewHandler(gen *generation) *previewHandler {
	return &previewHandler{generationHolder{current: gen}}
}

// previewTemplateData is what previewPageTemplate renders. Fields are exported only because html/template requires it
type previewTemplateData struct {
	LayerName          string
	TileURL            string
	HasBounds          bool
	South              float64
	North              float64
	West               float64
	East               float64
	Center             []float64 // longitude, latitude, optional zoom
	MinZoom            int
	MaxZoom            int
	IsVector           bool
	VectorSourceLayers []string
	SourceLayerKnown   bool
	FromMetadata       bool
	Attribution        string
}

//go:embed preview_handler.html.tmpl
var previewPageHTML string

var previewPageTemplate = template.Must(template.New("preview").Parse(previewPageHTML))

func previewBounds(doc layer.TileJSONDocument) (bool, pkg.Bounds) {
	bounds := pkg.Bounds{West: doc.Bounds[0], South: doc.Bounds[1], East: doc.Bounds[2], North: doc.Bounds[3], SRID: pkg.SRIDWGS84}

	return bounds != pkg.WorldBounds(), bounds
}

func previewSourceLayers(vectorLayers []config.VectorLayer) []string {
	ids := make([]string, 0, len(vectorLayers))
	for _, v := range vectorLayers {
		if v.ID != "" {
			ids = append(ids, v.ID)
		}
	}

	return ids
}

func (h *previewHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	slog.DebugContext(ctx, "server: preview handler started")
	defer slog.DebugContext(ctx, "server: preview handler ended")

	cur, release := h.acquire()
	defer release()

	cur.writeHeaders(w)

	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if !cur.auth().CheckAuthentication(ctx, req) {
		writeError(ctx, w, &cur.errCfg, pkg.UnauthorizedError{Message: "CheckAuthentication returned false"}, config.DataTypeUnknown)
		return
	}

	name := req.PathValue("layer")

	l := cur.layerGroup().FindLayer(ctx, name)
	if l == nil {
		writeError(ctx, w, &cur.errCfg, pkg.UnauthorizedError{Message: "Layer " + name + " does not exist"}, config.DataTypeUnknown)
		return
	}

	limitLayers, allowed := layerRestriction(ctx)
	if limitLayers && !layerNameAllowed(name, l.ID, allowed) {
		writeError(ctx, w, &cur.errCfg, pkg.UnauthorizedError{Message: "Denying access to non-allowed layer"}, config.DataTypeUnknown)
		return
	}

	publicURL := resolvePublicURLs(req, cur.serverCfg.TileJSON.BaseURLs)[0]
	tilePathPrefix := cur.tilePathPrefix()
	tileURL := publicURL.build(tilePathPrefix + "/" + name + "/{z}/{x}/{y}")

	doc := l.BuildTileJSON(name, nil, areaRestriction(ctx))
	hasBounds, bounds := previewBounds(doc)

	// Without ?name= or provider metadata, guess the source-layer the way postgis_mvt defaults it;
	// the page then probes a sample tile to verify the guess.
	sourceLayers := []string{name}
	sourceLayerKnown := false
	fromMetadata := false
	if override := req.URL.Query().Get("name"); override != "" {
		sourceLayers = []string{override}
		sourceLayerKnown = true
	} else if ids := previewSourceLayers(doc.VectorLayers); len(ids) > 0 {
		sourceLayers = ids
		sourceLayerKnown = true
		fromMetadata = true
	}

	var center []float64
	if !hasBounds && len(doc.Center) >= 2 {
		center = doc.Center
	}

	data := previewTemplateData{
		LayerName:          name,
		TileURL:            tileURL,
		HasBounds:          hasBounds,
		South:              bounds.South,
		North:              bounds.North,
		West:               bounds.West,
		East:               bounds.East,
		Center:             center,
		MinZoom:            doc.MinZoom,
		MaxZoom:            doc.MaxZoom,
		IsVector:           l.DataType == config.DataTypeMVT,
		VectorSourceLayers: sourceLayers,
		SourceLayerKnown:   sourceLayerKnown,
		FromMetadata:       fromMetadata,
		Attribution:        doc.Attribution,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if err := previewPageTemplate.Execute(w, data); err != nil {
		slog.WarnContext(ctx, "Unable to write to preview request due to "+err.Error())
	}
}
