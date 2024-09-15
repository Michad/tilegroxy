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
	"slices"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgisMvtConfig struct {
	Host       string
	Port       uint16
	User       string
	Password   string
	Database   string
	Table      string
	Resolution uint
	Buffer     float64
	GID        string
	Geometry   string
	Attributes []string
	Filter     string
	SourceSRID uint
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

	cfg.Host = "127.0.0.1"
	cfg.Port = 5432
	cfg.User = "postgresql"
	cfg.Password = ""
	cfg.Database = ""
	cfg.Table = ""
	cfg.Resolution = 4096
	cfg.Buffer = 0.125
	cfg.GID = "gid"
	cfg.Geometry = "geom"
	cfg.Attributes = []string{}
	cfg.SourceSRID = pkg.SRIDWGS84

	return cfg
}

func (s PostgisMvtRegistration) Name() string {
	return "postgismvt"
}

func (s PostgisMvtRegistration) Initialize(cfgAny any, _ config.ClientConfig, _ config.ErrorMessages, _ *layer.LayerGroup) (layer.Provider, error) {
	cfg := cfgAny.(PostgisMvtConfig)

	dbCfg, err := pgxpool.ParseConfig("")

	if err != nil {
		return nil, err
	}

	dbCfg.ConnConfig.Host = cfg.Host
	dbCfg.ConnConfig.Port = cfg.Port
	dbCfg.ConnConfig.Database = cfg.Database
	dbCfg.ConnConfig.User = cfg.User
	dbCfg.ConnConfig.Password = cfg.Password
	dbCfg.ConnConfig.Host = cfg.Host

	dbpool, err := pgxpool.NewWithConfig(pkg.BackgroundContext(), dbCfg)

	if err != nil {
		return nil, err
	}

	return &PostgisMvt{cfg, dbpool}, nil
}

func (t PostgisMvt) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return layer.ProviderContext{AuthBypass: true}, nil
}

func (t PostgisMvt) GenerateTile(ctx context.Context, _ layer.ProviderContext, req pkg.TileRequest) (*pkg.Image, error) {
	conn, err := t.pool.Acquire(ctx)
	defer conn.Release()

	if err != nil {
		return nil, err
	}

	bounds, err := req.GetBoundsProjection(pkg.SRIDPsuedoMercator)

	if err != nil {
		return nil, err
	}
	rawEnv := bounds.ToEWKT()
	bufEnv := bounds.BufferRelative(t.Buffer).ToEWKT()

	params := []any{int(t.SourceSRID), rawEnv, t.Resolution, int(t.Buffer * float64(t.Resolution)), bufEnv, req.LayerName, t.GID}

	query := `WITH mvtgeom AS(SELECT ST_AsMVTGeom(ST_Transform(ST_SetSRID("` + t.Geometry + `", $1::integer), 3857), $2::geometry, extent => $3, buffer => $4) AS geom`

	query += `, "`
	query += t.GID
	query += `" `

	for _, col := range t.Attributes {
		query += `, "`
		query += col
		query += `" `
	}

	query += `FROM ` + t.Table
	query += ` WHERE "` + t.Geometry + `" && ST_Transform($5::geometry, $1::integer) AND ST_Intersects("` + t.Geometry + `", ST_Transform($5::geometry, $1::integer))`

	if t.Filter != "" {
		preparedFilter, replacements, err := replacePlaceholdersInString(ctx, req, t.Filter, 8, false, t.SourceSRID)
		if err != nil {
			return nil, err
		}

		query += " AND " + preparedFilter

		if len(replacements) > 0 {
			params = slices.Concat(params, replacements)
		}
	}

	query += ") SELECT ST_AsMVT(mvtgeom.*, $6, $3, 'geom', $7) FROM mvtgeom"

	if slog.Default().Enabled(ctx, config.LevelTrace) {
		queryDebug := query
		for i := range params {
			realI := len(params) - i - 1
			queryDebug = strings.ReplaceAll(queryDebug, "$"+strconv.Itoa(i+1), fmt.Sprint(params[realI]))
		}

		slog.Log(ctx, config.LevelTrace, queryDebug)
	}

	row := conn.QueryRow(ctx, query, params...)

	var result []byte
	err = row.Scan(&result)

	if slog.Default().Enabled(ctx, config.LevelAbsurd) {
		slog.Log(ctx, config.LevelAbsurd, string(result))
	}

	return &pkg.Image{Content: result, ContentType: "application/vnd.mapbox-vector-tile"}, err
}
