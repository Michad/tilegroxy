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

package analytics

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Postgres_Names(t *testing.T) {
	assert.Equal(t, "postgresql", PostgresRegistration{}.Name())
	assert.Equal(t, "postgres", PostgresLegacyRegistration{}.Name())

	for _, name := range []string{"postgresql", "postgres"} {
		reg, ok := analytics.RegisteredAnalytics(name)
		require.True(t, ok, name)
		assert.IsType(t, PostgresConfig{}, reg.InitializeConfig())
	}
}

// The deprecated alias must behave identically to the canonical registration
func Test_Postgres_LegacyAliasInitializes(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	empty, err := datastore.ConstructDatastoreRegistry(nil, nil, msgs)
	require.NoError(t, err)

	reg, ok := analytics.RegisteredAnalytics("postgres")
	require.True(t, ok)

	_, err = reg.Initialize(PostgresConfig{Datastore: "nonexistent", Table: "t"}, empty, msgs)
	require.ErrorContains(t, err, "analytics.postgresql.datastore")
}
