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

package audit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capture installs a logger writing into a buffer and restores the previous state afterwards, so
// tests don't leak a destination into each other.
func capture(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	SetAuditLoggerOnStartup(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { SetAuditLoggerOnStartup(nil) })

	return &buf
}

func Test_DisabledByDefault(t *testing.T) {
	assert.False(t, Enabled())

	// Emitting with no logger installed must not panic
	AuthFailure(context.Background(), "no logger")
	ConfigReload(context.Background(), nil)
}

func Test_AuthFailureRecordsReason(t *testing.T) {
	buf := capture(t)

	AuthFailure(pkg.BackgroundContext(), "CheckAuthentication returned false")

	out := buf.String()
	assert.Contains(t, out, EventAuthFailure)
	assert.Contains(t, out, "CheckAuthentication returned false")
}

func Test_AuthFailureIncludesIdentityWhenKnown(t *testing.T) {
	buf := capture(t)

	ctx := pkg.BackgroundContext()
	user, ok := pkg.UserIDFromContext(ctx)
	require.True(t, ok)
	*user = "someone"
	tenant, ok := pkg.TenantIDFromContext(ctx)
	require.True(t, ok)
	*tenant = "acme"

	AuthFailure(ctx, "Denying access to non-allowed layer")

	out := buf.String()
	assert.Contains(t, out, `"user":"someone"`)
	assert.Contains(t, out, `"tenant":"acme"`)
}

func Test_AuthFailureOmitsIdentityWhenUnknown(t *testing.T) {
	buf := capture(t)

	AuthFailure(pkg.BackgroundContext(), "CheckAuthentication returned false")

	out := buf.String()
	assert.NotContains(t, out, `"user":`)
	assert.NotContains(t, out, `"tenant":`)
}

func Test_ConfigReloadSuccess(t *testing.T) {
	buf := capture(t)

	ConfigReload(pkg.BackgroundContext(), nil)

	out := buf.String()
	assert.Contains(t, out, EventConfigReload)
	assert.Contains(t, out, OutcomeSuccess)
}

func Test_ConfigReloadFailureRecordsError(t *testing.T) {
	buf := capture(t)

	ConfigReload(pkg.BackgroundContext(), errors.New("layer osm is invalid"))

	out := buf.String()
	assert.Contains(t, out, EventConfigReload)
	assert.Contains(t, out, OutcomeFailure)
	assert.Contains(t, out, "layer osm is invalid")
}
