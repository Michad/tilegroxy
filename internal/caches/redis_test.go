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
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	//This is a hack to help with vscode test execution. Put a .env in repo root w/ anything you need for test containers
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

func setupRedisContainer(ctx context.Context, t *testing.T) (testcontainers.Container, func(t *testing.T)) {
	t.Log("setup container")

	req := testcontainers.ContainerRequest{
		Image:        "redis:latest",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}
	redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if !assert.NoError(t, err) {
		return nil, nil
	}

	return redisC, func(t *testing.T) {
		t.Log("teardown container")

		err := redisC.Terminate(ctx)
		assert.NoError(t, err)
	}
}

func TestRedisWithContainerHostAndPort(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.NoError(t, err) {
		return
	}

	cfg := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
	}

	r, err := RedisRegistration{}.Initialize(cfg, config.ClientConfig{}, config.ErrorMessages{})
	if !assert.NoError(t, err) {
		return
	}

	validateSaveAndLookup(t, r)
}

func TestRedisWithContainerSingleServersArr(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.NoError(t, err) {
		return
	}

	cfg := RedisConfig{
		Servers: []HostAndPort{extractHostAndPort(t, endpoint)},
	}

	r, err := RedisRegistration{}.Initialize(cfg, config.ClientConfig{}, config.ErrorMessages{})
	if !assert.NoError(t, err) {
		return
	}

	validateSaveAndLookup(t, r)
}

func TestRedisWithContainerRing(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	redisC2, cleanupF2 := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) || !assert.NotNil(t, redisC2) {
		return
	}

	defer cleanupF(t)
	defer cleanupF2(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.NoError(t, err) {
		return
	}
	endpoint2, err := redisC2.Endpoint(ctx, "")
	if !assert.NoError(t, err) {
		return
	}

	cfg := RedisConfig{
		Mode:    ModeRing,
		Servers: []HostAndPort{extractHostAndPort(t, endpoint), extractHostAndPort(t, endpoint2)},
	}

	r, err := RedisRegistration{}.Initialize(cfg, config.ClientConfig{}, config.ErrorMessages{})
	if !assert.NoError(t, err) {
		return
	}

	validateSaveAndLookup(t, r)
}

func TestRedisWithContainerDiffPrefix(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.NoError(t, err) {
		return
	}

	cfg := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "first_",
	}

	r, err := RedisRegistration{}.Initialize(cfg, config.ClientConfig{}, config.ErrorMessages{})
	if !assert.NoError(t, err) {
		return
	}

	config2 := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "second_",
	}

	r2, err := RedisRegistration{}.Initialize(config2, config.ClientConfig{}, config.ErrorMessages{})
	if !assert.NoError(t, err) {
		return
	}

	_ = validateSaveAndLookup(t, r) &&
		validateSaveAndLookup(t, r2)
}
func TestRedisWithContainerDiffDb(t *testing.T) {
	ctx := context.Background()
	redisC, cleanupF := setupRedisContainer(ctx, t)
	if !assert.NotNil(t, redisC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.NoError(t, err) {
		return
	}

	cfg := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		Db:          0,
	}

	r, err := RedisRegistration{}.Initialize(cfg, config.ClientConfig{}, config.ErrorMessages{})
	if !assert.NoError(t, err) {
		return
	}

	config2 := RedisConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		Db:          1,
	}

	r2, err := RedisRegistration{}.Initialize(config2, config.ClientConfig{}, config.ErrorMessages{})
	_ = assert.NoError(t, err) &&
		validateSaveAndLookup(t, r) &&
		validateSaveAndLookup(t, r2)
}
