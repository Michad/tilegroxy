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

package datastores

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

// The wire protocols clickhouse-go can speak
const (
	ClickhouseProtocolNative = "native"
	ClickhouseProtocolHTTP   = "http"
)

var AllClickhouseProtocols = []string{ClickhouseProtocolNative, ClickhouseProtocolHTTP}

type ClickhouseWrapperConfig struct {
	ID             string
	Host           string
	Port           uint16
	User           string
	Password       string
	Database       string
	MaxConnections int
	MinConnections int
	IdleTimeout    int32 // In seconds
	Lifetime       int32 // In seconds
	Secure         bool  // Connect over TLS
	Protocol       string
}

type ClickhouseWrapper struct {
	ClickhouseWrapperConfig
	conn driver.Conn
}

func init() {
	datastore.RegisterDatastoreWrapper(ClickhouseWrapperRegistration{})
}

type ClickhouseWrapperRegistration struct {
}

//nolint:mnd
func (s ClickhouseWrapperRegistration) InitializeConfig() any {
	cfg := ClickhouseWrapperConfig{}

	cfg.Host = "127.0.0.1"
	cfg.Port = 9000
	cfg.User = "default"
	cfg.Password = ""
	cfg.Database = "default"
	cfg.MinConnections = 5
	cfg.MaxConnections = 10
	cfg.IdleTimeout = 60 * 10
	cfg.Lifetime = 60 * 60
	cfg.Protocol = ClickhouseProtocolNative

	return cfg
}

func (s ClickhouseWrapperRegistration) Name() string {
	return "clickhouse"
}

func (s ClickhouseWrapperRegistration) Initialize(cfgAny any, _ secret.Secreter, errorMessages config.ErrorMessages) (datastore.DatastoreWrapper, error) {
	cfg := cfgAny.(ClickhouseWrapperConfig)

	var proto clickhouse.Protocol

	switch cfg.Protocol {
	case ClickhouseProtocolNative:
		proto = clickhouse.Native
	case ClickhouseProtocolHTTP:
		proto = clickhouse.HTTP
	default:
		return nil, fmt.Errorf(errorMessages.EnumError, "datastore.clickhouse.protocol", cfg.Protocol, AllClickhouseProtocols)
	}

	opts := &clickhouse.Options{
		Addr:     []string{cfg.Host + ":" + strconv.Itoa(int(cfg.Port))},
		Protocol: proto,
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		MaxOpenConns:    cfg.MaxConnections,
		MaxIdleConns:    cfg.MinConnections,
		ConnMaxLifetime: time.Duration(cfg.Lifetime) * time.Second,
	}

	if cfg.Secure {
		opts.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	conn, err := clickhouse.Open(opts)

	if err != nil {
		return nil, err
	}

	return &ClickhouseWrapper{cfg, conn}, nil
}

func (p ClickhouseWrapper) GetID() string {
	return p.ID
}

func (p ClickhouseWrapper) Native() any {
	return p.conn
}

// Close shuts down the connection pool so a hot reload doesn't leak it
func (p ClickhouseWrapper) Close(_ context.Context) error {
	if p.conn == nil {
		return nil
	}

	return p.conn.Close()
}
