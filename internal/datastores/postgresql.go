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

package datastores

import (
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresqlWrapperConfig struct {
	ID       string
	Host     string
	Port     uint16
	User     string
	Password string
	Database string
}

type PostgresqlWrapper struct {
	PostgresqlWrapperConfig
	pool *pgxpool.Pool
}

func init() {
	datastore.RegisterDatastoreWrapper(PostgresqlWrapperRegistration{})
}

type PostgresqlWrapperRegistration struct {
}

func (s PostgresqlWrapperRegistration) InitializeConfig() any {
	cfg := PostgresqlWrapperConfig{}

	cfg.Host = "127.0.0.1"
	cfg.Port = 5432
	cfg.User = "postgresql"
	cfg.Password = ""
	cfg.Database = ""

	return cfg
}

func (s PostgresqlWrapperRegistration) Name() string {
	return "postgresql"
}

func (s PostgresqlWrapperRegistration) Initialize(cfgAny any, _ secret.Secreter, _ config.ErrorMessages) (datastore.DatastoreWrapper, error) {
	cfg := cfgAny.(PostgresqlWrapperConfig)

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

	return &PostgresqlWrapper{cfg, dbpool}, nil
}

func (p PostgresqlWrapper) GetID() string {
	return p.ID
}

func (p PostgresqlWrapper) Native() any {
	return p.pool
}
