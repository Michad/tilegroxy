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
	"github.com/stretchr/testify/require"
)

type recordingCache struct {
	removed []pkg.TileRequest
	err     error
	present bool
}

func (c *recordingCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, nil
}

func (c *recordingCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error {
	return nil
}

func (c *recordingCache) Remove(_ context.Context, t pkg.TileRequest) (bool, error) {
	c.removed = append(c.removed, t)
	return c.present, c.err
}

func Test_CacheWrapper_RemoveForwardsToWrapped(t *testing.T) {
	inner := &recordingCache{present: true}
	w := CacheWrapper{Name: "stub", Cache: inner}
	req := pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3}

	removed, err := w.Remove(context.Background(), req)
	require.NoError(t, err)
	require.True(t, removed)
	require.Equal(t, []pkg.TileRequest{req}, inner.removed)
}

func Test_CacheWrapper_RemovePropagatesError(t *testing.T) {
	expected := errors.New("intentional test error")
	w := CacheWrapper{Name: "stub", Cache: &recordingCache{err: expected}}

	removed, err := w.Remove(context.Background(), pkg.TileRequest{LayerName: "layer", Z: 1, X: 2, Y: 3})
	require.ErrorIs(t, err, expected)
	require.False(t, removed)
}
