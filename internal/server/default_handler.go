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
	"log/slog"
	"net/http"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type reloadableEntities struct {
	config     *config.Config
	layerGroup *layer.LayerGroup
	auth       authentication.Authentication
	analytics  *analytics.AnalyticsWrapper
	// The full set this generation came from, retained so the previous generation can be released
	// after a reload swaps it out.
	all *entities.Entities
	// The refcounted generation this projection belongs to. Requests hold it for their duration so
	// a reload cannot release entities out from under them.
	gen *generation
}

// newReloadableEntities projects a constructed set of entities into the subset the handlers use.
func newReloadableEntities(cfg *config.Config, ent *entities.Entities, gen *generation) reloadableEntities {
	r := reloadableEntities{config: cfg, all: ent, gen: gen}

	if ent != nil {
		r.layerGroup = ent.LayerGroup
		r.auth = ent.Auth
		r.analytics = ent.Analytics
	}

	return r
}

type defaultHandler struct {
	reloadableEntities
}

func (h *defaultHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	slog.DebugContext(ctx, "server: default handler started")
	defer slog.DebugContext(ctx, "server: default handler ended")

	if h.config.Server.DocsPath != "" {
		w.Header().Add("Location", h.config.Server.RootPath+h.config.Server.DocsPath)
		w.WriteHeader(http.StatusTemporaryRedirect)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}
