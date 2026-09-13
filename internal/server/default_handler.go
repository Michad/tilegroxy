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
	// The server section is not reloadable, so this is the configuration the process started with,
	// carried across every reload. Held by value: the handlers have no path back to the live
	// *config.Config, so a reload cannot reach what they serve.
	serverCfg config.ServerConfig
	// Only error.messages and error.images are reloadable, so a reload keeps the startup mode and
	// alwaysok rather than applying halves of a section documented as needing a restart.
	errCfg     config.ErrorConfig
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
// Only valid at startup: a reload must go through reloadedFrom so the non-reloadable values carry
// forward.
func newReloadableEntities(cfg *config.Config, ent *entities.Entities, gen *generation) reloadableEntities {
	r := reloadableEntities{all: ent, gen: gen}

	if cfg != nil {
		r.serverCfg = cfg.Server
		r.errCfg = cfg.Error
	}

	if ent != nil {
		r.layerGroup = ent.LayerGroup
		r.auth = ent.Auth
		r.analytics = ent.Analytics
	}

	return r
}

// reloadedFrom builds the projection a reload swaps in. Everything non-reloadable is taken from
// the current projection rather than the new config, so the handlers keep serving what the routes
// registered at startup describe.
func (h reloadableEntities) reloadedFrom(cfg *config.Config, ent *entities.Entities, gen *generation) reloadableEntities {
	next := newReloadableEntities(cfg, ent, gen)
	next.serverCfg = h.serverCfg
	next.errCfg.Mode = h.errCfg.Mode
	next.errCfg.AlwaysOK = h.errCfg.AlwaysOK

	return next
}

// tilePathPrefix is the path tiles are advertised under, matching the route setupHandlers
// registered at startup.
func (h reloadableEntities) tilePathPrefix() string {
	return h.serverCfg.RootPath + h.serverCfg.TilePath
}

type defaultHandler struct {
	reloadableEntities
}

func (h *defaultHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	slog.DebugContext(ctx, "server: default handler started")
	defer slog.DebugContext(ctx, "server: default handler ended")

	if h.serverCfg.DocsPath != "" {
		w.Header().Add("Location", h.serverCfg.RootPath+h.serverCfg.DocsPath)
		w.WriteHeader(http.StatusTemporaryRedirect)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}
