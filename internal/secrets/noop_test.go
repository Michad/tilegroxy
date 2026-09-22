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

package secrets

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Noop_LookupErrorsWithEmptyVersion(t *testing.T) {
	s, err := NoopRegistration{}.Initialize(NoopConfig{}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{ParamRequired: "%v is required"}})
	require.NoError(t, err)

	value, version, err := s.Lookup(context.Background(), "anything")
	require.Error(t, err)
	assert.Empty(t, value)
	assert.Empty(t, version)
}

// Empty versions are how a backend says it cannot detect changes
func Test_Noop_CheckReturnsEmptyVersions(t *testing.T) {
	s, err := NoopRegistration{}.Initialize(NoopConfig{}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	versions, err := s.Check(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"", ""}, versions)
}

func Test_Noop_CannotBeWatched(t *testing.T) {
	assert.Equal(t, 0, NoopRegistration{}.CheckBatchSize())
}
