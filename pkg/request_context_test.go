// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pkg_test

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_CopyAuthRestrictions(t *testing.T) {
	from := pkg.BackgroundContext()
	to := pkg.BackgroundContext()

	limitLayers, ok := pkg.LimitLayersFromContext(from)
	require.True(t, ok)
	*limitLayers = true
	allowedLayers, ok := pkg.AllowedLayersFromContext(from)
	require.True(t, ok)
	*allowedLayers = []string{"a", "b"}
	partial, ok := pkg.LimitAreaPartialFromContext(from)
	require.True(t, ok)
	*partial = true
	area, ok := pkg.AllowedAreaFromContext(from)
	require.True(t, ok)
	*area = pkg.Bounds{South: 1, North: 2, West: 3, East: 4}
	user, ok := pkg.UserIDFromContext(from)
	require.True(t, ok)
	*user = "someone"
	tenant, ok := pkg.TenantIDFromContext(from)
	require.True(t, ok)
	*tenant = "acme-corp"

	pkg.CopyAuthRestrictions(from, to)

	newLimitLayers, _ := pkg.LimitLayersFromContext(to)
	assert.True(t, *newLimitLayers)
	newAllowedLayers, _ := pkg.AllowedLayersFromContext(to)
	assert.Equal(t, []string{"a", "b"}, *newAllowedLayers)
	newPartial, _ := pkg.LimitAreaPartialFromContext(to)
	assert.True(t, *newPartial)
	newArea, _ := pkg.AllowedAreaFromContext(to)
	assert.Equal(t, pkg.Bounds{South: 1, North: 2, West: 3, East: 4}, *newArea)
	newUser, _ := pkg.UserIDFromContext(to)
	assert.Equal(t, "someone", *newUser)
	newTenant, _ := pkg.TenantIDFromContext(to)
	assert.Equal(t, "acme-corp", *newTenant)
}

// Copying must not alias, so a later change to one context can't reach the other.
func Test_CopyAuthRestrictions_DoesNotAlias(t *testing.T) {
	from := pkg.BackgroundContext()
	to := pkg.BackgroundContext()

	pkg.CopyAuthRestrictions(from, to)

	area, _ := pkg.AllowedAreaFromContext(to)
	*area = pkg.Bounds{South: 5, North: 6, West: 7, East: 8}

	fromArea, _ := pkg.AllowedAreaFromContext(from)
	assert.Equal(t, pkg.Bounds{}, *fromArea)
}

// A context missing the auth values entirely, as happens for anything not built by
// NewRequestContext, must be a no-op rather than a panic.
func Test_CopyAuthRestrictions_MissingValues(t *testing.T) {
	to := pkg.BackgroundContext()

	assert.NotPanics(t, func() { pkg.CopyAuthRestrictions(t.Context(), to) })
	assert.NotPanics(t, func() { pkg.CopyAuthRestrictions(to, t.Context()) })
}

// A freshly built request context has an empty, non-nil tenant ID, mirroring UserIDFromContext's
// default so field resolvers can treat "unset" and "empty string" the same way.
func Test_TenantIDFromContext_DefaultsToEmptyString(t *testing.T) {
	ctx := pkg.BackgroundContext()

	tenant, ok := pkg.TenantIDFromContext(ctx)
	require.True(t, ok)
	assert.Empty(t, *tenant)
}
