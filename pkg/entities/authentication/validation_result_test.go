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

package authentication_test

import (
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/stretchr/testify/assert"
)

func Test_ValidationResult_FieldsAreIndependentlyAddressable(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	r := authentication.ValidationResult{
		Pass:          true,
		Expiration:    exp,
		UserID:        "user-1",
		TenantID:      "tenant-1",
		AllowedLayers: []string{"osm"},
	}

	assert.True(t, r.Pass)
	assert.Equal(t, exp, r.Expiration)
	assert.Equal(t, "user-1", r.UserID)
	assert.Equal(t, "tenant-1", r.TenantID)
	assert.Equal(t, []string{"osm"}, r.AllowedLayers)
}

func Test_ValidationResult_ZeroValueIsUnauthenticated(t *testing.T) {
	var r authentication.ValidationResult

	assert.False(t, r.Pass)
	assert.Empty(t, r.UserID)
	assert.Empty(t, r.TenantID)
	assert.Empty(t, r.AllowedLayers)
}
