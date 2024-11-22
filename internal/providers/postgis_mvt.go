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

package providers

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/layer"

	"github.com/jackc/pgx/v5/pgxpool"
)

// column names come explicitly from config which are trusted operator inputs. So this isn't intended to be a primary protection against SQL injection attacks, just a helper against mistakes
var columnRegex = regexp.MustCompile("^[a-zA-Z0-9_]+$")

type PostgisMvtConfig struct {
	Layer      string
	Datastore  string
	Table      string
	Extent     uint16
	Buffer     float64
	GID        string
	Geometry   string
	Attributes []string
	Filter     string
	SourceSRID uint
	Limit      uint16
}

type PostgisMvt struct {
	PostgisMvtConfig
	pool *pgxpool.Pool
}

func init() {
	layer.RegisterProvider(PostgisMvtRegistration{})
}

type PostgisMvtRegistration struct {
}

func (s PostgisMvtRegistration) InitializeConfig() any {
	cfg := PostgisMvtConfig{}

	cfg.Table = ""
	cfg.Extent = 4096
	cfg.Buffer = 0.125
	cfg.GID = "gid"
	cfg.Geometry = "geom"
	cfg.Attributes = []string{}
	cfg.SourceSRID = pkg.SRIDWGS84
	cfg.Limit = 0

	return cfg
}

func (s PostgisMvtRegistration) Name() string {
	return "postgismvt"
}

func (s PostgisMvtRegistration) Initialize(cfgAny any, _ config.ClientConfig, errorMessages config.ErrorMessages, _ *layer.LayerGroup, datastores *datastore.DatastoreRegistry) (layer.Provider, error) {
	cfg := cfgAny.(PostgisMvtConfig)

	if cfg.GID != "" {
		if !columnRegex.MatchString(cfg.GID) {
			return nil, fmt.Errorf(errorMessages.InvalidParam, "postgismvt.gid", cfg.GID, columnRegex)
		}
	}
	if cfg.Geometry != "" {
		if !columnRegex.MatchString(cfg.Geometry) {
			return nil, fmt.Errorf(errorMessages.InvalidParam, "postgismvt.geometry", cfg.Geometry, columnRegex)
		}
	}
	if len(cfg.Attributes) > 0 {
		for i, attribute := range cfg.Attributes {
			if !columnRegex.MatchString(attribute) {
				return nil, fmt.Errorf(errorMessages.InvalidParam, "postgismvt.attributes."+strconv.Itoa(i), attribute, columnRegex)
			}
		}
	}

	var dbpool *pgxpool.Pool

	ds, ok := datastores.Get(cfg.Datastore)
	if ok {
		dbpool, ok = ds.Native().(*pgxpool.Pool)
	}

	if !ok {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "postgismvt.datastore", cfg.Datastore)
	}

	return &PostgisMvt{cfg, dbpool}, nil
}

func (t PostgisMvt) PreAuth(_ context.Context, _ layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func (t PostgisMvt) GenerateTile(ctx context.Context, _ layer.ProviderContext, req pkg.TileRequest) (*pkg.Image, error) {
	conn, err := t.pool.Acquire(ctx)

	if err != nil {
		return nil, err
	}

	defer conn.Release()

	bounds, err := req.GetBoundsProjection(pkg.SRIDPsuedoMercator)

	if err != nil {
		return nil, err
	}
	rawEnv := bounds.ToEWKT()
	bufEnv := bounds.BufferRelative(t.Buffer).ConfineToPsuedoMercatorRange().ToEWKT()

	if t.SourceSRID > math.MaxInt {
		return nil, pkg.InvalidSridError{}
	}

	layerName := t.Layer
	if layerName == "" {
		layerName = req.LayerName
	}

	params := []any{int(t.SourceSRID), rawEnv, t.Extent, int(t.Buffer * float64(t.Extent)), bufEnv, layerName, t.GID} // #nosec G115

	query := `WITH mvtgeom AS(SELECT ST_AsMVTGeom(ST_Transform(ST_SetSRID("` + t.Geometry + `", $1::integer), 3857), $2::geometry, extent => $3, buffer => $4) AS "geom"`

	query += `, "`
	query += t.GID
	query += `" `

	if len(t.Attributes) > 0 {
		for _, col := range t.Attributes {
			query += `, "`
			query += col
			query += `" `
		}
	}

	query += `FROM ` + t.Table

	// Doing both an explicit BBox check and ST_Intersects isn't strictly necessary but I've seen cases where it helps the query planner
	query += ` WHERE "` + t.Geometry + `" && ST_Transform($5::geometry, $1::integer) AND ST_Intersects("` + t.Geometry + `", ST_Transform($5::geometry, $1::integer))`

	if t.Filter != "" {
		preparedFilter, replacements, err := replacePlaceholdersInString(ctx, req, t.Filter, len(params)+1, false, t.SourceSRID)
		if err != nil {
			return nil, err
		}

		query += " AND " + preparedFilter

		if len(replacements) > 0 {
			params = slices.Concat(params, replacements)
		}
	}

	if t.Limit > 0 {
		query += " LIMIT " + strconv.Itoa(int(t.Limit))
	}

	query += ") SELECT ST_AsMVT(mvtgeom.*, $6, $3, 'geom', $7) FROM mvtgeom"

	if slog.Default().Enabled(ctx, config.LevelTrace) {
		queryDebug := query
		for i := range params {
			realI := len(params) - i - 1
			queryDebug = strings.ReplaceAll(queryDebug, "$"+strconv.Itoa(realI+1), "'"+fmt.Sprint(params[realI])+"'")
		}

		slog.Log(ctx, config.LevelTrace, queryDebug)
	}

	row := conn.QueryRow(ctx, query, params...)

	var result []byte
	err = row.Scan(&result)

	if slog.Default().Enabled(ctx, config.LevelAbsurd) {
		slog.Log(ctx, config.LevelAbsurd, string(result))
	}

	return &pkg.Image{Content: result, ContentType: mvtContentType}, err
}
