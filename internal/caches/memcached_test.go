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

package caches

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupMemcachedContainer(ctx context.Context, t *testing.T) (testcontainers.Container, func(t *testing.T)) {
	t.Log("setup container")

	req := testcontainers.ContainerRequest{
		Image:        "memcached:latest",
		ExposedPorts: []string{"11211/tcp"},
		WaitingFor:   wait.ForExposedPort(),
	}
	memcachedC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if !assert.Nil(t, err) {
		return nil, nil
	}

	return memcachedC, func(t *testing.T) {
		t.Log("teardown container")

		err := memcachedC.Terminate(ctx)
		assert.Nil(t, err)
	}
}

func TestMemcachedWithContainerHostAndPort(t *testing.T) {
	ctx := context.Background()
	memcachedC, cleanupF := setupMemcachedContainer(ctx, t)
	if !assert.NotNil(t, memcachedC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcachedC.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}

	config := MemcachedConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
	}

	r, err := ConstructMemcached(&config, nil)
	_ = assert.Nil(t, err) &&
		validateSaveAndLookup(t, r)
}

func TestMemcachedWithContainerSingleServersArr(t *testing.T) {
	ctx := context.Background()
	memcachedC, cleanupF := setupMemcachedContainer(ctx, t)
	if !assert.NotNil(t, memcachedC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcachedC.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}

	config := MemcachedConfig{
		Servers: []HostAndPort{extractHostAndPort(t, endpoint)},
	}

	r, err := ConstructMemcached(&config, nil)
	_ = assert.Nil(t, err) &&
		validateSaveAndLookup(t, r)
}

func TestMemcachedWithContainerDiffPrefix(t *testing.T) {
	ctx := context.Background()
	memcachedC, cleanupF := setupMemcachedContainer(ctx, t)
	if !assert.NotNil(t, memcachedC) {
		return
	}

	defer cleanupF(t)

	endpoint, err := memcachedC.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}

	config := MemcachedConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "first_",
	}

	r, err := ConstructMemcached(&config, nil)
	if !assert.Nil(t, err) {
		return
	}

	config2 := MemcachedConfig{
		HostAndPort: extractHostAndPort(t, endpoint),
		KeyPrefix:   "second_",
	}

	r2, err := ConstructMemcached(&config2, nil)
	_ = assert.Nil(t, err) &&
		validateSaveAndLookup(t, r) &&
		validateSaveAndLookup(t, r2)
}
