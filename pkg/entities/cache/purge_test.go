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
	"errors"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// purgeable and notPurgeable also implement Cache so they can double as the wrapped cache in
// wrapper_test.go's CacheWrapper tests.
type purgeable struct {
	purgedLayer string
	err         error
}

func (p *purgeable) Purge(_ context.Context, layerName string) error {
	p.purgedLayer = layerName
	return p.err
}

func (p *purgeable) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (p *purgeable) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	return nil
}

type notPurgeable struct{}

func (notPurgeable) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (notPurgeable) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	return nil
}

func Test_PurgeIfPurgeable(t *testing.T) {
	ctx := context.Background()

	p := &purgeable{}
	ok, err := PurgeIfPurgeable(ctx, p, "osm")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "osm", p.purgedLayer)

	// Most caches don't support purging; that must be reported via the bool rather than an error,
	// since it's an expected, non-error outcome.
	ok, err = PurgeIfPurgeable(ctx, notPurgeable{}, "osm")
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = PurgeIfPurgeable(ctx, nil, "osm")
	require.NoError(t, err)
	assert.False(t, ok)

	failing := &purgeable{err: errors.New("could not purge")}
	ok, err = PurgeIfPurgeable(ctx, failing, "osm")
	require.Error(t, err)
	assert.True(t, ok)
}
