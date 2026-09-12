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

package tg

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spyCache is a no-op Cache that counts Close calls, registered through the same entry point
// operator-supplied caches use. It's the only way to observe whether the reload callback actually
// released a generation's resources, since configToEntities builds the real thing and nothing in
// serve.go can be swapped out for a mock.
type spyCache struct {
	closes *atomic.Int32
}

func (spyCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) { return nil, nil }
func (spyCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error   { return nil }
func (c spyCache) Close(_ context.Context) error {
	c.closes.Add(1)
	return nil
}

type spyCacheRegistration struct {
	closes *atomic.Int32
}

func (spyCacheRegistration) InitializeConfig() any { return struct{}{} }
func (spyCacheRegistration) Name() string          { return "spy" }
func (r spyCacheRegistration) Initialize(_ any, _ cache.CacheDeps) (cache.Cache, error) {
	return spyCache(r), nil
}

// spyCacheConfig returns a DefaultConfig wired to a freshly registered spy cache so each test gets
// its own independent close counter.
func spyCacheConfig(t *testing.T) (config.Config, *atomic.Int32) {
	t.Helper()

	closes := &atomic.Int32{}
	cache.RegisterCache(spyCacheRegistration{closes: closes})

	cfg := config.DefaultConfig()
	cfg.Cache = map[string]interface{}{"name": "spy"}

	return cfg, closes
}

// A generation that's built but never wins the swap has no handler to release it later, so the
// reload callback must close it itself rather than leaking its connection pools.
func Test_ReloadClosesGenerationWhenSwapFails(t *testing.T) {
	cfg, closes := spyCacheConfig(t)

	swapErr := errors.New("swap rejected")

	// Stand in for the server's reload callback, which can fail after entities are built.
	var nextReload = func(_ *config.Config, _ *entities.Entities) error {
		return swapErr
	}

	callback := newReloadCallback(&nextReload)

	err := callback(&cfg)

	require.ErrorIs(t, err, swapErr)
	assert.Equal(t, int32(1), closes.Load(), "a generation that never started serving must have its cache closed")
}

// A successful swap hands the generation to its new owner, so the reload callback must not also
// close it out from under that owner.
func Test_ReloadDoesNotCloseGenerationWhenSwapSucceeds(t *testing.T) {
	cfg, closes := spyCacheConfig(t)

	var nextReload = func(_ *config.Config, _ *entities.Entities) error {
		return nil
	}

	callback := newReloadCallback(&nextReload)

	err := callback(&cfg)

	require.NoError(t, err)
	assert.Equal(t, int32(0), closes.Load(), "a generation that is now serving must not be closed")
}

// configToEntities failing on config validation happens before anything is constructed, so there
// is nothing to close and swap must not run at all.
func Test_ReloadDoesNotCloseWhenBuildFails(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Error.Mode = "not-a-real-mode"

	swapCalled := false
	var nextReload = func(_ *config.Config, _ *entities.Entities) error {
		swapCalled = true
		return nil
	}

	callback := newReloadCallback(&nextReload)

	err := callback(&cfg)

	require.Error(t, err)
	assert.False(t, swapCalled, "swap must not run when the generation never got built")
}

// configToEntities failing after the cache was already constructed - because a later entity like
// auth has an invalid config - must close the cache it already built rather than leaking it, since
// nothing else will ever get a chance to release it.
func Test_ReloadClosesAlreadyBuiltEntitiesWhenBuildFailsPartway(t *testing.T) {
	cfg, closes := spyCacheConfig(t)
	cfg.Authentication = map[string]interface{}{"name": "not-a-real-auth-provider"}

	swapCalled := false
	var nextReload = func(_ *config.Config, _ *entities.Entities) error {
		swapCalled = true
		return nil
	}

	callback := newReloadCallback(&nextReload)

	err := callback(&cfg)

	require.Error(t, err)
	assert.False(t, swapCalled, "swap must not run when the generation never finished building")
	assert.Equal(t, int32(1), closes.Load(), "the cache built before the later failure must still be closed")
}

// Before the server publishes a reload target, *nextReloadPtr is nil and the callback must be a
// no-op rather than panicking on the nil call.
func Test_ReloadCallback_NoopBeforeReloadTargetPublished(t *testing.T) {
	cfg := config.DefaultConfig()

	var nextReload func(*config.Config, *entities.Entities) error

	callback := newReloadCallback(&nextReload)

	assert.NoError(t, callback(&cfg))
}

func Test_ReloadAuditsSuccess(t *testing.T) {
	var buf bytes.Buffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	cfg, _ := spyCacheConfig(t)

	var nextReload = func(_ *config.Config, _ *entities.Entities) error { return nil }

	require.NoError(t, newReloadCallback(&nextReload)(&cfg))

	out := buf.String()
	assert.Contains(t, out, audit.EventConfigReload)
	assert.Contains(t, out, audit.OutcomeSuccess)
}

// A reload that never gets built and one that fails to swap both have to leave a trail, otherwise
// the audit log would imply the new config took effect.
func Test_ReloadAuditsBuildFailure(t *testing.T) {
	var buf bytes.Buffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	cfg := config.DefaultConfig()
	cfg.Error.Mode = "not-a-real-mode"

	var nextReload = func(_ *config.Config, _ *entities.Entities) error { return nil }

	require.Error(t, newReloadCallback(&nextReload)(&cfg))

	out := buf.String()
	assert.Contains(t, out, audit.EventConfigReload)
	assert.Contains(t, out, audit.OutcomeFailure)
}

func Test_ReloadAuditsSwapFailure(t *testing.T) {
	var buf bytes.Buffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	cfg, _ := spyCacheConfig(t)

	swapErr := errors.New("swap failed")
	var nextReload = func(_ *config.Config, _ *entities.Entities) error { return swapErr }

	require.ErrorIs(t, newReloadCallback(&nextReload)(&cfg), swapErr)

	out := buf.String()
	assert.Contains(t, out, audit.EventConfigReload)
	assert.Contains(t, out, audit.OutcomeFailure)
	assert.Contains(t, out, "swap failed")
}
