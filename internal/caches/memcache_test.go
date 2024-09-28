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

//go:build !unit

package caches

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	// This is a hack to help with vscode test execution. Put a .env in repo root w/ anything you need for test containers
	if env, err := os.ReadFile("../../.env"); err == nil {
		envs := strings.Split(string(env), "\n")
		for _, e := range envs {
			if es := strings.Split(e, "="); len(es) == 2 {
				fmt.Printf("Loading env...")
				os.Setenv(es[0], es[1])
			}
		}
	}
}

func setupMemcacheContainer(ctx context.Context, t *testing.T) (testcontainers.Container, func(t *testing.T)) {
	t.Log("setup container")

	req := testcontainers.ContainerRequest{
		Image:        "memcached:latest",
		ExposedPorts: []string{"11211/tcp"},
		WaitingFor:   wait.ForExposedPort(),
	}
	memcacheC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	return memcacheC, func(t *testing.T) {
		t.Log("teardown container")

		err := memcacheC.Terminate(ctx)
		require.NoError(t, err)
	}
}

func TestMemcacheWithContainerHostAndPort(t *testing.T) {
	ctx := context.Background()
	memcacheC, cleanupF := setupMemcacheContainer(ctx, t)
	if !assert.NotNil(t, memcacheC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcacheC.Endpoint(ctx, "")
	require.NoError(t, err)

	cfg := MemcacheConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
	}

	r, err := MemcacheRegistration{}.Initialize(cfg, config.ErrorMessages{})
	require.NoError(t, err)
	validateSaveAndLookup(t, r)
}

func TestMemcacheWithContainerSingleServersArr(t *testing.T) {
	ctx := context.Background()
	memcacheC, cleanupF := setupMemcacheContainer(ctx, t)
	if !assert.NotNil(t, memcacheC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcacheC.Endpoint(ctx, "")
	require.NoError(t, err)

	cfg := MemcacheConfig{
		Servers: []HostAndPort{extractHostAndPort(t, endpoint)},
	}

	r, err := MemcacheRegistration{}.Initialize(cfg, config.ErrorMessages{})
	require.NoError(t, err)
	validateSaveAndLookup(t, r)
}

func TestMemcacheWithContainerDiffPrefix(t *testing.T) {
	ctx := context.Background()
	memcacheC, cleanupF := setupMemcacheContainer(ctx, t)
	if !assert.NotNil(t, memcacheC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcacheC.Endpoint(ctx, "")
	require.NoError(t, err)

	cfg := MemcacheConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "first_",
	}

	r, err := MemcacheRegistration{}.Initialize(cfg, config.ErrorMessages{})
	require.NoError(t, err)

	config2 := MemcacheConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "second_",
	}

	r2, err := MemcacheRegistration{}.Initialize(config2, config.ErrorMessages{})
	require.NoError(t, err)
	validateSaveAndLookup(t, r)
	validateSaveAndLookup(t, r2)
}
