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

package entry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/internal/entities"
	"github.com/Michad/tilegroxy/internal/secrets"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Counts closes. Registered like operator caches since that's the only way to observe whether a reload released a generation
type spyCache struct {
	closes *atomic.Int32
}

func (spyCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) { return nil, nil }
func (spyCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error   { return nil }
func (spyCache) Remove(_ context.Context, _ pkg.TileRequest) (bool, error)       { return false, nil }
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

// Each test gets its own spy registration and close counter
func spyCacheConfig(t *testing.T) (config.Config, *atomic.Int32) {
	t.Helper()

	closes := &atomic.Int32{}
	cache.RegisterCache(spyCacheRegistration{closes: closes})

	cfg := config.DefaultConfig()
	cfg.Cache = map[string]interface{}{"name": "spy"}

	return cfg, closes
}

// A generation that loses the swap has no other owner, so the callback must close it or leak its pools
func Test_ReloadClosesGenerationWhenSwapFails(t *testing.T) {
	cfg, closes := spyCacheConfig(t)

	swapErr := errors.New("swap rejected")

	// Stands in for the server's reload callback, which can fail after entities are built
	var nextReload = func(_ *config.Config, _ *entities.Entities) error {
		return swapErr
	}

	callback := readyReloader(&cfg, nextReload).apply

	err := callback(&cfg, audit.ReasonConfigFile)

	require.ErrorIs(t, err, swapErr)
	assert.Equal(t, int32(1), closes.Load(), "a generation that never started serving must have its cache closed")
}

// A successful swap transfers ownership, so the callback must not also close the generation
func Test_ReloadDoesNotCloseGenerationWhenSwapSucceeds(t *testing.T) {
	cfg, closes := spyCacheConfig(t)

	var nextReload = func(_ *config.Config, _ *entities.Entities) error {
		return nil
	}

	callback := readyReloader(&cfg, nextReload).apply

	err := callback(&cfg, audit.ReasonConfigFile)

	require.NoError(t, err)
	assert.Equal(t, int32(0), closes.Load(), "a generation that is now serving must not be closed")
}

// Validation fails before anything is constructed, so nothing is closed and swap never runs
func Test_ReloadDoesNotCloseWhenBuildFails(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Error.Mode = "not-a-real-mode"

	swapCalled := false
	var nextReload = func(_ *config.Config, _ *entities.Entities) error {
		swapCalled = true
		return nil
	}

	callback := readyReloader(&cfg, nextReload).apply

	err := callback(&cfg, audit.ReasonConfigFile)

	require.Error(t, err)
	assert.False(t, swapCalled, "swap must not run when the generation never got built")
}

// A later entity failing must close the already-built cache since nothing else can release it
func Test_ReloadClosesAlreadyBuiltEntitiesWhenBuildFailsPartway(t *testing.T) {
	cfg, closes := spyCacheConfig(t)
	cfg.Authentication = map[string]interface{}{"name": "not-a-real-auth-provider"}

	swapCalled := false
	var nextReload = func(_ *config.Config, _ *entities.Entities) error {
		swapCalled = true
		return nil
	}

	callback := readyReloader(&cfg, nextReload).apply

	err := callback(&cfg, audit.ReasonConfigFile)

	require.Error(t, err)
	assert.False(t, swapCalled, "swap must not run when the generation never finished building")
	assert.Equal(t, int32(1), closes.Load(), "the cache built before the later failure must still be closed")
}

// Reload is public and may run on a caller's goroutine, so panics must return as errors instead of crashing
func Test_ReloadCallback_RecoversPanic(t *testing.T) {
	cfg := config.DefaultConfig()

	var nextReload = func(_ *config.Config, _ *entities.Entities) error {
		panic("simulated panic from a buggy reload target")
	}

	callback := readyReloader(&cfg, nextReload).apply

	err := callback(&cfg, audit.ReasonConfigFile)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated panic from a buggy reload target")
}

func Test_ReloadAuditsSuccess(t *testing.T) {
	var buf bytes.Buffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	cfg, _ := spyCacheConfig(t)

	var nextReload = func(_ *config.Config, _ *entities.Entities) error { return nil }

	require.NoError(t, readyReloader(&cfg, nextReload).apply(&cfg, audit.ReasonConfigFile))

	out := buf.String()
	assert.Contains(t, out, audit.EventConfigReload)
	assert.Contains(t, out, audit.OutcomeSuccess)
}

// Otherwise the audit log would imply the new config took effect
func Test_ReloadAuditsBuildFailure(t *testing.T) {
	var buf bytes.Buffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	cfg := config.DefaultConfig()
	cfg.Error.Mode = "not-a-real-mode"

	var nextReload = func(_ *config.Config, _ *entities.Entities) error { return nil }

	require.Error(t, readyReloader(&cfg, nextReload).apply(&cfg, audit.ReasonConfigFile))

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

	require.ErrorIs(t, readyReloader(&cfg, nextReload).apply(&cfg, audit.ReasonConfigFile), swapErr)

	out := buf.String()
	assert.Contains(t, out, audit.EventConfigReload)
	assert.Contains(t, out, audit.OutcomeFailure)
	assert.Contains(t, out, "swap failed")
}

func Test_ConfigToEntities_PassesReloadFuncToSecreter(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Secret = map[string]interface{}{"name": "none"}

	called := false
	ent, err := configToEntities(pkg.BackgroundContext(), cfg, func(_ string) { called = true })
	require.NoError(t, err)
	t.Cleanup(func() { _ = ent.Close(context.Background()) })

	// "none" can't be watched, so there's no wrapper or callback, but construction must still succeed
	assert.False(t, called)
	require.NotNil(t, ent.Secreter)
}

// Secret rotation after a file reload must re-resolve the newly applied config, not the startup one
func Test_SecretReloadUsesLatestConfigAfterFileReload(t *testing.T) {
	startupCfg, _ := spyCacheConfig(t)
	newCfg := startupCfg
	newCfg.Server.Port = 9999

	seen := make(chan int, 2)
	var nextReload = func(c *config.Config, _ *entities.Entities) error {
		seen <- c.Server.Port
		return nil
	}

	reloader := readyReloader(&startupCfg, nextReload)

	require.NoError(t, reloader.Reload(&newCfg))
	assert.Equal(t, newCfg.Server.Port, <-seen)

	reloader.requestLive(secrets.ReasonSecretRotation)

	select {
	case port := <-seen:
		assert.Equal(t, newCfg.Server.Port, port, "the secret reload must re-apply the hot-reloaded config")
	case <-time.After(5 * time.Second):
		t.Fatal("secret reload never ran")
	}

	settle(t, reloader)
}

// Otherwise a rotation and a file edit are indistinguishable after the fact
func Test_ReloadAuditsSecretRotationReason(t *testing.T) {
	var buf bytes.Buffer
	audit.SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	cfg, _ := spyCacheConfig(t)

	var nextReload = func(_ *config.Config, _ *entities.Entities) error { return nil }

	require.NoError(t, readyReloader(&cfg, nextReload).apply(&cfg, secrets.ReasonSecretRotation))

	assert.Contains(t, buf.String(), secrets.ReasonSecretRotation)
}
