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

package cache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CacheWrapper itself must NOT satisfy Purgeable: it wraps every cache regardless of backend, so
// if it always exposed Purge, a type assertion against Purgeable could never distinguish a
// purgeable backend from one that doesn't support it. Callers must check the wrapped Cache field.
func Test_CacheWrapper_DoesNotItselfSatisfyPurgeable(t *testing.T) {
	w := CacheWrapper{Name: "stub", Cache: &purgeable{}}

	_, ok := any(w).(Purgeable)
	assert.False(t, ok)
}

func Test_CacheWrapper_WrappedPurgeableCacheIsReachableThroughTheField(t *testing.T) {
	p := &purgeable{}
	w := CacheWrapper{Name: "stub", Cache: p}

	ok, err := PurgeIfPurgeable(context.Background(), w.Cache, "osm")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "osm", p.purgedLayer)
}

func Test_CacheWrapper_WrappedNonPurgeableCacheReportsUnsupported(t *testing.T) {
	w := CacheWrapper{Name: "stub", Cache: notPurgeable{}}

	ok, err := PurgeIfPurgeable(context.Background(), w.Cache, "osm")
	require.NoError(t, err)
	assert.False(t, ok)
}
