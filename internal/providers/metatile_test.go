// Copyright 2025 Michael Davis
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

package providers

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/encoding/mvt"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/maptile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingProvider records how many times the wrapped fetch actually happens and can stall to let
// a test line several sub-tile requests up against one in-flight metatile fetch.
type countingProvider struct {
	calls    atomic.Int32
	requests chan pkg.TileRequest
	release  chan struct{}
	img      func(pkg.TileRequest) *pkg.Image
	err      error
}

func (c *countingProvider) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return providerContext, nil
}

func (c *countingProvider) GenerateTile(ctx context.Context, _ layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	c.calls.Add(1)

	if c.requests != nil {
		c.requests <- tileRequest
	}

	if c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if c.err != nil {
		return nil, c.err
	}

	return c.img(tileRequest), nil
}

// newMetatile builds the provider directly so tests can supply a fake inner provider rather than
// going through config decoding.
func newMetatile(t *testing.T, size int, inner layer.Provider, ttl time.Duration) *Metatile {
	t.Helper()

	m, err := MetatileRegistration{}.Initialize(MetatileConfig{
		Size:     size,
		Provider: map[string]interface{}{"name": "static", "color": "F00"},
	}, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	require.NoError(t, err)

	meta, ok := m.(*Metatile)
	require.True(t, ok)

	meta.provider = inner
	meta.ttl = ttl

	return meta
}

func Test_DataType_Metatile(t *testing.T) {
	assert.Equal(t, config.DataTypeRaster, MetatileRegistration{}.DataType(MetatileConfig{
		Provider: map[string]interface{}{"name": "static", "color": "F00"},
	}))
	assert.Equal(t, config.DataTypeUnknown, MetatileRegistration{}.DataType(MetatileConfig{}))
	assert.Equal(t, config.DataTypeUnknown, MetatileRegistration{}.DataType("not a config"))
	assert.Equal(t, "metatile", MetatileRegistration{}.Name())
	assert.Equal(t, MetatileConfig{}, MetatileRegistration{}.InitializeConfig())
}

func Test_MetatileCoordinates(t *testing.T) {
	tests := []struct {
		name      string
		req       pkg.TileRequest
		zoomDelta int
		wantMeta  pkg.TileRequest
		wantCol   int
		wantRow   int
		wantOK    bool
	}{
		{"2x2 top left quadrant", pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 4}, 1, pkg.TileRequest{LayerName: "l", Z: 4, X: 4, Y: 2}, 0, 0, true},
		{"2x2 top right quadrant", pkg.TileRequest{LayerName: "l", Z: 5, X: 9, Y: 4}, 1, pkg.TileRequest{LayerName: "l", Z: 4, X: 4, Y: 2}, 1, 0, true},
		{"2x2 bottom left quadrant", pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 5}, 1, pkg.TileRequest{LayerName: "l", Z: 4, X: 4, Y: 2}, 0, 1, true},
		{"2x2 bottom right quadrant", pkg.TileRequest{LayerName: "l", Z: 5, X: 9, Y: 5}, 1, pkg.TileRequest{LayerName: "l", Z: 4, X: 4, Y: 2}, 1, 1, true},
		{"4x4 grid interior", pkg.TileRequest{LayerName: "l", Z: 6, X: 22, Y: 9}, 2, pkg.TileRequest{LayerName: "l", Z: 4, X: 5, Y: 2}, 2, 1, true},
		{"4x4 grid last cell", pkg.TileRequest{LayerName: "l", Z: 6, X: 23, Y: 11}, 2, pkg.TileRequest{LayerName: "l", Z: 4, X: 5, Y: 2}, 3, 3, true},
		{"8x8 grid", pkg.TileRequest{LayerName: "l", Z: 10, X: 511, Y: 300}, 3, pkg.TileRequest{LayerName: "l", Z: 7, X: 63, Y: 37}, 7, 4, true},
		{"origin tile", pkg.TileRequest{LayerName: "l", Z: 3, X: 0, Y: 0}, 1, pkg.TileRequest{LayerName: "l", Z: 2, X: 0, Y: 0}, 0, 0, true},
		{"last tile in world", pkg.TileRequest{LayerName: "l", Z: 3, X: 7, Y: 7}, 1, pkg.TileRequest{LayerName: "l", Z: 2, X: 3, Y: 3}, 1, 1, true},
		{"exactly reaches zoom 0", pkg.TileRequest{LayerName: "l", Z: 1, X: 1, Y: 0}, 1, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0}, 1, 0, true},
		{"zoom 2 with delta 2 reaches zoom 0", pkg.TileRequest{LayerName: "l", Z: 2, X: 3, Y: 2}, 2, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0}, 3, 2, true},
		{"zoom 0 has nothing larger", pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0}, 1, pkg.TileRequest{LayerName: "l", Z: 0, X: 0, Y: 0}, 0, 0, false},
		{"delta past zoom 0", pkg.TileRequest{LayerName: "l", Z: 1, X: 1, Y: 1}, 2, pkg.TileRequest{LayerName: "l", Z: 1, X: 1, Y: 1}, 0, 0, false},
		{"size 1 is a passthrough", pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 4}, 0, pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 4}, 0, 0, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			meta, col, row, ok := metatileFor(test.req, test.zoomDelta)

			assert.Equal(t, test.wantOK, ok)
			assert.Equal(t, test.wantMeta, meta)
			assert.Equal(t, test.wantCol, col)
			assert.Equal(t, test.wantRow, row)
		})
	}
}

// Every tile in a metatile must map back to a distinct cell, and the metatile has to contain the
// requested tile geographically.
func Test_MetatileCoordinatesCoverGridExactly(t *testing.T) {
	for _, zoomDelta := range []int{1, 2, 3} {
		size := 1 << zoomDelta
		baseX, baseY := 4*size, 2*size
		seen := make(map[[2]int]bool)

		for dy := range size {
			for dx := range size {
				req := pkg.TileRequest{LayerName: "l", Z: 8, X: baseX + dx, Y: baseY + dy}
				meta, col, row, ok := metatileFor(req, zoomDelta)

				require.True(t, ok)
				assert.Equal(t, dx, col)
				assert.Equal(t, dy, row)
				assert.Equal(t, pkg.TileRequest{LayerName: "l", Z: 8 - zoomDelta, X: baseX >> zoomDelta, Y: baseY >> zoomDelta}, meta)
				assert.False(t, seen[[2]int{col, row}], "duplicate cell")
				seen[[2]int{col, row}] = true

				// The metatile's extent has to actually contain the requested tile's extent.
				reqBounds, err := req.GetBounds()
				require.NoError(t, err)
				metaBounds, err := meta.GetBounds()
				require.NoError(t, err)
				assert.True(t, metaBounds.Contains(*reqBounds), "metatile must contain the sub-tile")
			}
		}

		assert.Len(t, seen, size*size)
	}
}

func Test_MetatileValidate(t *testing.T) {
	inner := map[string]interface{}{"name": "static", "color": "F00"}
	deps := layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages}

	for _, size := range []int{0, -1, 3, 6, 33, 100} {
		c, err := MetatileRegistration{}.Initialize(MetatileConfig{Size: size, Provider: inner}, deps)
		assert.Nil(t, c, "size %v should be rejected", size)
		require.Error(t, err, "size %v should be rejected", size)
	}

	for _, size := range []int{1, 2, 4, 8, 16, 32} {
		c, err := MetatileRegistration{}.Initialize(MetatileConfig{Size: size, Provider: inner}, deps)
		assert.NotNil(t, c, "size %v should be accepted", size)
		require.NoError(t, err, "size %v should be accepted", size)
	}

	// An unusable inner provider has to fail at initialize rather than at request time.
	c, err := MetatileRegistration{}.Initialize(MetatileConfig{Size: 2, Provider: map[string]interface{}{"name": "notreal"}}, deps)
	assert.Nil(t, c)
	require.Error(t, err)
}

func Test_MetatileTTLConfig(t *testing.T) {
	inner := map[string]interface{}{"name": "static", "color": "F00"}
	deps := layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages}

	c, err := MetatileRegistration{}.Initialize(MetatileConfig{Size: 2, Provider: inner}, deps)
	require.NoError(t, err)
	assert.Equal(t, defaultMetatileTTL, c.(*Metatile).ttl)

	ttl := 5
	c, err = MetatileRegistration{}.Initialize(MetatileConfig{Size: 2, CacheTTLSeconds: &ttl, Provider: inner}, deps)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, c.(*Metatile).ttl)
}

// gridImage paints each cell of a sizeXsize grid a distinct color so a crop can be identified.
func gridImage(t *testing.T, size, cellPixels int, colorFor func(col, row int) color.RGBA) *pkg.Image {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, size*cellPixels, size*cellPixels))
	for row := range size {
		for col := range size {
			c := colorFor(col, row)
			for y := range cellPixels {
				for x := range cellPixels {
					img.Set(col*cellPixels+x, row*cellPixels+y, c)
				}
			}
		}
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	require.NoError(t, png.Encode(writer, img))
	require.NoError(t, writer.Flush())

	return &pkg.Image{Content: buf.Bytes(), ContentType: mimePng}
}

func decodeFirstPixel(t *testing.T, img *pkg.Image) color.RGBA {
	t.Helper()

	realImage, _, err := image.Decode(bytes.NewReader(img.Content))
	require.NoError(t, err)

	r, g, b, a := realImage.At(0, 0).RGBA()

	// RGBA returns 16 bit values, so the high byte of each is the 8 bit channel.
	return color.RGBA{byte(r >> 8 & 0xFF), byte(g >> 8 & 0xFF), byte(b >> 8 & 0xFF), byte(a >> 8 & 0xFF)}
}

// The right quadrant has to come back for each sub-tile position.
func Test_MetatileRasterSplitsCorrectQuadrant(t *testing.T) {
	colors := map[[2]int]color.RGBA{
		{0, 0}: {255, 0, 0, 255},
		{1, 0}: {0, 255, 0, 255},
		{0, 1}: {0, 0, 255, 255},
		{1, 1}: {255, 255, 0, 255},
	}

	inner := &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return gridImage(t, 2, 128, func(col, row int) color.RGBA { return colors[[2]int{col, row}] })
	}}

	// TTL off so each sub-tile fetches its own copy, isolating the crop from the caching.
	m := newMetatile(t, 2, inner, 0)

	cases := []struct {
		x, y     int
		col, row int
	}{
		{8, 4, 0, 0},
		{9, 4, 1, 0},
		{8, 5, 0, 1},
		{9, 5, 1, 1},
	}

	for _, c := range cases {
		img, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: c.x, Y: c.y})
		require.NoError(t, err)
		require.NotNil(t, img)

		assert.Equal(t, colors[[2]int{c.col, c.row}], decodeFirstPixel(t, img), "tile %v/%v should be the %v,%v cell", c.x, c.y, c.col, c.row)

		realImage, _, err := image.Decode(bytes.NewReader(img.Content))
		require.NoError(t, err)
		assert.Equal(t, 128, realImage.Bounds().Dx())
		assert.Equal(t, 128, realImage.Bounds().Dy())
	}
}

func Test_MetatileRasterSplits4x4(t *testing.T) {
	// Encodes the cell position into the color so any mix-up is visible.
	colorFor := func(col, row int) color.RGBA {
		return color.RGBA{byte((10 + col*20) & 0xFF), byte((10 + row*20) & 0xFF), 100, 255}
	}

	inner := &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return gridImage(t, 4, 64, colorFor)
	}}
	m := newMetatile(t, 4, inner, 0)

	baseX, baseY := 20, 12
	for row := range 4 {
		for col := range 4 {
			img, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 6, X: baseX + col, Y: baseY + row})
			require.NoError(t, err)
			assert.Equal(t, colorFor(col, row), decodeFirstPixel(t, img), "cell %v,%v", col, row)
		}
	}
}

// The whole point of the feature: N*N sub-tiles must produce exactly one upstream fetch.
func Test_MetatileCoalescesConcurrentSubtiles(t *testing.T) {
	inner := &countingProvider{
		requests: make(chan pkg.TileRequest, 16),
		release:  make(chan struct{}),
		img: func(_ pkg.TileRequest) *pkg.Image {
			return gridImage(t, 2, 32, func(_, _ int) color.RGBA { return color.RGBA{1, 2, 3, 255} })
		},
	}
	m := newMetatile(t, 2, inner, time.Minute)

	subTiles := []pkg.TileRequest{
		{LayerName: "l", Z: 5, X: 8, Y: 4},
		{LayerName: "l", Z: 5, X: 9, Y: 4},
		{LayerName: "l", Z: 5, X: 8, Y: 5},
		{LayerName: "l", Z: 5, X: 9, Y: 5},
	}

	// Hold the first fetch open so the rest pile up behind it rather than hitting the TTL cache.
	var wg sync.WaitGroup
	errs := make([]error, len(subTiles))
	imgs := make([]*pkg.Image, len(subTiles))

	wg.Add(1)
	go func() {
		defer wg.Done()
		imgs[0], errs[0] = m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, subTiles[0])
	}()

	// Wait for the first fetch to actually be in flight before starting the others.
	metaReq := <-inner.requests
	assert.Equal(t, pkg.TileRequest{LayerName: "l", Z: 4, X: 4, Y: 2}, metaReq)

	for i := 1; i < len(subTiles); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			imgs[i], errs[i] = m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, subTiles[i])
		}()
	}

	// Give the waiters a moment to reach the singleflight before letting the fetch finish. If any
	// of them started its own fetch instead, the call count below catches it.
	time.Sleep(50 * time.Millisecond)

	close(inner.release)
	wg.Wait()

	for i := range subTiles {
		require.NoError(t, errs[i])
		require.NotNil(t, imgs[i])
	}

	assert.Equal(t, int32(1), inner.calls.Load(), "all four sub-tiles should share one upstream fetch")
}

// One caller's context being cancelled while it happens to be the singleflight leader must not
// abort the fetch for every other sub-tile still waiting on the same metatile.
func Test_MetatileLeaderCancellationDoesNotAbortOtherWaiters(t *testing.T) {
	inner := &countingProvider{
		requests: make(chan pkg.TileRequest, 16),
		release:  make(chan struct{}),
		img: func(_ pkg.TileRequest) *pkg.Image {
			return gridImage(t, 2, 32, func(_, _ int) color.RGBA { return color.RGBA{1, 2, 3, 255} })
		},
	}
	m := newMetatile(t, 2, inner, time.Minute)

	subTiles := []pkg.TileRequest{
		{LayerName: "l", Z: 5, X: 8, Y: 4},
		{LayerName: "l", Z: 5, X: 9, Y: 4},
		{LayerName: "l", Z: 5, X: 8, Y: 5},
	}

	leaderCtx, cancelLeader := context.WithCancel(pkg.BackgroundContext())

	var wg sync.WaitGroup
	errs := make([]error, len(subTiles))
	imgs := make([]*pkg.Image, len(subTiles))

	// subTiles[0] triggers the fetch and becomes the singleflight leader.
	wg.Add(1)
	go func() {
		defer wg.Done()
		imgs[0], errs[0] = m.GenerateTile(leaderCtx, layer.ProviderContext{}, subTiles[0])
	}()

	metaReq := <-inner.requests
	assert.Equal(t, pkg.TileRequest{LayerName: "l", Z: 4, X: 4, Y: 2}, metaReq)

	// The rest join as waiters on their own, uncancelled contexts.
	for i := 1; i < len(subTiles); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			imgs[i], errs[i] = m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, subTiles[i])
		}()
	}

	time.Sleep(50 * time.Millisecond)

	// Cancel the leader's own request context while the fetch is still in flight.
	cancelLeader()
	time.Sleep(50 * time.Millisecond)

	close(inner.release)
	wg.Wait()

	require.ErrorIs(t, errs[0], context.Canceled, "the cancelled caller should see its own cancellation")

	for i := 1; i < len(subTiles); i++ {
		require.NoError(t, errs[i], "an uncancelled waiter must still get the shared fetch's result")
		require.NotNil(t, imgs[i])
	}

	assert.Equal(t, int32(1), inner.calls.Load(), "the leader's cancellation must not trigger a second upstream fetch")
}

// Distinct metatiles must not be coalesced into each other.
func Test_MetatileDoesNotCoalesceDistinctMetatiles(t *testing.T) {
	inner := &countingProvider{
		requests: make(chan pkg.TileRequest, 64),
		img: func(_ pkg.TileRequest) *pkg.Image {
			return gridImage(t, 2, 32, func(_, _ int) color.RGBA { return color.RGBA{1, 2, 3, 255} })
		},
	}
	m := newMetatile(t, 2, inner, time.Minute)

	// Four separate metatiles, each with all four of its sub-tiles requested concurrently.
	var wg sync.WaitGroup
	for _, base := range [][2]int{{8, 4}, {10, 4}, {8, 6}, {12, 8}} {
		for dy := range 2 {
			for dx := range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: base[0] + dx, Y: base[1] + dy})
					assert.NoError(t, err)
				}()
			}
		}
	}
	wg.Wait()

	assert.Equal(t, int32(4), inner.calls.Load(), "each distinct metatile needs its own fetch")

	close(inner.requests)
	seen := make(map[pkg.TileRequest]bool)
	for req := range inner.requests {
		assert.False(t, seen[req], "metatile %v fetched more than once", req)
		seen[req] = true
		assert.Equal(t, 4, req.Z)
	}
	assert.Len(t, seen, 4)
}

func Test_MetatileCachesAcrossSequentialRequests(t *testing.T) {
	inner := &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return gridImage(t, 2, 32, func(_, _ int) color.RGBA { return color.RGBA{1, 2, 3, 255} })
	}}
	m := newMetatile(t, 2, inner, time.Minute)

	for dy := range 2 {
		for dx := range 2 {
			_, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: 8 + dx, Y: 4 + dy})
			require.NoError(t, err)
		}
	}

	assert.Equal(t, int32(1), inner.calls.Load())
}

func Test_MetatileExpiredEntryRefetches(t *testing.T) {
	inner := &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return gridImage(t, 2, 32, func(_, _ int) color.RGBA { return color.RGBA{1, 2, 3, 255} })
	}}
	m := newMetatile(t, 2, inner, time.Millisecond)

	req := pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 4}
	_, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, req)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	_, err = m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, req)
	require.NoError(t, err)

	assert.Equal(t, int32(2), inner.calls.Load(), "an expired metatile should be fetched again")
}

// A metatile flagged to skip caching must not be retained for its siblings.
func Test_MetatileRespectsForceSkipCache(t *testing.T) {
	inner := &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		img := gridImage(t, 2, 32, func(_, _ int) color.RGBA { return color.RGBA{1, 2, 3, 255} })
		img.ForceSkipCache = true
		return img
	}}
	m := newMetatile(t, 2, inner, time.Minute)

	for dy := range 2 {
		for dx := range 2 {
			img, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: 8 + dx, Y: 4 + dy})
			require.NoError(t, err)
			assert.True(t, img.ForceSkipCache, "the flag should carry through to the sub-tile")
		}
	}

	assert.Equal(t, int32(4), inner.calls.Load())
}

// Below the zoom delta there is no larger tile, so the request passes through untouched.
func Test_MetatilePassesThroughNearZoomZero(t *testing.T) {
	var got pkg.TileRequest
	inner := &countingProvider{img: func(req pkg.TileRequest) *pkg.Image {
		got = req
		return gridImage(t, 1, 64, func(_, _ int) color.RGBA { return color.RGBA{9, 9, 9, 255} })
	}}
	m := newMetatile(t, 4, inner, time.Minute)

	req := pkg.TileRequest{LayerName: "l", Z: 1, X: 1, Y: 0}
	img, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, req)
	require.NoError(t, err)
	require.NotNil(t, img)

	assert.Equal(t, req, got, "the original request should reach the inner provider unchanged")
	assert.Equal(t, int32(1), inner.calls.Load())
}

func Test_MetatileSizeOnePassesThrough(t *testing.T) {
	var got pkg.TileRequest
	inner := &countingProvider{img: func(req pkg.TileRequest) *pkg.Image {
		got = req
		return gridImage(t, 1, 64, func(_, _ int) color.RGBA { return color.RGBA{9, 9, 9, 255} })
	}}
	m := newMetatile(t, 1, inner, time.Minute)

	req := pkg.TileRequest{LayerName: "l", Z: 7, X: 40, Y: 20}
	_, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, req)
	require.NoError(t, err)

	assert.Equal(t, req, got)
}

func Test_MetatileErrors(t *testing.T) {
	m := newMetatile(t, 2, &countingProvider{err: errors.New("upstream boom")}, time.Minute)

	// An invalid tile request is rejected before any metatile math happens.
	_, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: 99999, Y: 4})
	require.Error(t, err)

	_, err = m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: -1, X: 0, Y: 0})
	require.Error(t, err)

	// An upstream failure propagates and isn't retained.
	_, err = m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 4})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upstream boom")

	_, err = m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 4})
	require.Error(t, err, "a failed fetch should not be cached as a success")

	// Undecodable content surfaces as an error rather than a panic.
	m2 := newMetatile(t, 2, &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return &pkg.Image{Content: []byte("not an image"), ContentType: mimePng}
	}}, time.Minute)
	_, err = m2.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 4})
	require.Error(t, err)

	// An image too small to divide is reported rather than producing an empty tile.
	m3 := newMetatile(t, 4, &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return gridImage(t, 1, 2, func(_, _ int) color.RGBA { return color.RGBA{1, 1, 1, 255} })
	}}, time.Minute)
	_, err = m3.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 6, X: 20, Y: 12})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too small")
}

func Test_MetatilePreAuth(t *testing.T) {
	m := newMetatile(t, 2, &countingProvider{}, time.Minute)

	pc, err := m.PreAuth(pkg.BackgroundContext(), layer.ProviderContext{AuthToken: "tok"})
	require.NoError(t, err)
	assert.Equal(t, "tok", pc.AuthToken)
}

// mvtMetatile builds a vector metatile with one point per sub-tile cell, each placed at the centre
// of the cell it belongs to, so a split can be checked for putting the right point in each tile.
func mvtMetatile(t *testing.T, meta pkg.TileRequest, size int) *pkg.Image {
	t.Helper()

	fc := geojson.NewFeatureCollection()

	for row := range size {
		for col := range size {
			sub := pkg.TileRequest{LayerName: meta.LayerName, Z: meta.Z + intLog2(size), X: meta.X*size + col, Y: meta.Y*size + row}
			b, err := sub.GetBounds()
			require.NoError(t, err)

			lng, lat := b.Centroid()
			f := geojson.NewFeature(orb.Point{lng, lat})
			f.Properties["cell"] = cellName(col, row)
			fc.Append(f)
		}
	}

	layers := mvt.NewLayers(map[string]*geojson.FeatureCollection{"points": fc})
	//#nosec G115 -- test coordinates are small and known
	layers.ProjectToTile(maptile.New(uint32(meta.X), uint32(meta.Y), maptile.Zoom(meta.Z)))

	data, err := mvt.Marshal(layers)
	require.NoError(t, err)

	return &pkg.Image{Content: data, ContentType: mvtContentType}
}

func intLog2(n int) int {
	r := 0
	for n > 1 {
		n >>= 1
		r++
	}
	return r
}

// cellName labels a grid cell, e.g. "b2". Only used for grids small enough to stay in one digit.
func cellName(col, row int) string {
	letters := "abcdefghijklmnop"
	digits := "0123456789"

	return string(letters[col]) + string(digits[row])
}

// Each sub-tile must come back holding only the point that falls inside it, with the geometry
// re-projected onto the sub-tile rather than left in the metatile's coordinate space.
func Test_MetatileVectorSplit(t *testing.T) {
	meta := pkg.TileRequest{LayerName: "l", Z: 4, X: 4, Y: 2}
	inner := &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return mvtMetatile(t, meta, 2)
	}}
	m := newMetatile(t, 2, inner, 0)

	for row := range 2 {
		for col := range 2 {
			req := pkg.TileRequest{LayerName: "l", Z: 5, X: meta.X*2 + col, Y: meta.Y*2 + row}
			img, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, req)
			require.NoError(t, err)
			assert.Equal(t, mvtContentType, img.ContentType)

			layers, err := mvt.Unmarshal(img.Content)
			require.NoError(t, err)
			require.Len(t, layers, 1)

			//#nosec G115 -- test coordinates are small and known
			subTile := maptile.New(uint32(req.X), uint32(req.Y), maptile.Zoom(req.Z))
			layers.ProjectToWGS84(subTile)

			fc := layers[0]
			require.Len(t, fc.Features, 1, "cell %v,%v should keep exactly its own point", col, row)
			assert.Equal(t, cellName(col, row), fc.Features[0].Properties["cell"])

			// The surviving point has to land inside the requested tile's own bounds.
			bounds, err := req.GetBounds()
			require.NoError(t, err)
			pt, ok := fc.Features[0].Geometry.(orb.Point)
			require.True(t, ok)
			assert.True(t, bounds.ContainsPoint(pt[0], pt[1]), "point %v should be inside %v", pt, bounds)
		}
	}
}

func Test_MetatileVectorSplit4x4(t *testing.T) {
	meta := pkg.TileRequest{LayerName: "l", Z: 4, X: 5, Y: 2}
	inner := &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return mvtMetatile(t, meta, 4)
	}}
	m := newMetatile(t, 4, inner, 0)

	for row := range 4 {
		for col := range 4 {
			req := pkg.TileRequest{LayerName: "l", Z: 6, X: meta.X*4 + col, Y: meta.Y*4 + row}
			img, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, req)
			require.NoError(t, err)

			layers, err := mvt.Unmarshal(img.Content)
			require.NoError(t, err)
			require.Len(t, layers, 1)

			fc := layers[0]
			require.Len(t, fc.Features, 1, "cell %v,%v", col, row)
			assert.Equal(t, cellName(col, row), fc.Features[0].Properties["cell"])
		}
	}
}

func Test_MetatileVectorEmpty(t *testing.T) {
	inner := &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return &pkg.Image{Content: []byte{}, ContentType: mvtContentType}
	}}
	m := newMetatile(t, 2, inner, 0)

	img, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 4})
	require.NoError(t, err)
	assert.Empty(t, img.Content)
	assert.Equal(t, mvtContentType, img.ContentType)
}

func Test_MetatileVectorInvalid(t *testing.T) {
	inner := &countingProvider{img: func(_ pkg.TileRequest) *pkg.Image {
		return &pkg.Image{Content: []byte("nonsense"), ContentType: mvtContentType}
	}}
	m := newMetatile(t, 2, inner, 0)

	_, err := m.GenerateTile(pkg.BackgroundContext(), layer.ProviderContext{}, pkg.TileRequest{LayerName: "l", Z: 5, X: 8, Y: 4})
	require.Error(t, err)
}
