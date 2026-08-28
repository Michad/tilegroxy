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
	"fmt"
	"image"
	"image/png"
	"math"
	"math/bits"
	"strconv"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/paulmach/orb/encoding/mvt"
	"github.com/paulmach/orb/maptile"
	"golang.org/x/sync/singleflight"
)

// Upper bound on the metatile grid. 32x32 at the default 256 pixel tile size is already an 8192
// pixel fetch, past which a single upstream call gets unreasonable.
const maxMetatileSize = 32

// How long a fetched metatile stays available for its siblings after the fetch completes.
// Coalescing alone only helps sub-tiles that arrive while the fetch is in flight, and a client
// panning a viewport requests the siblings in quick succession rather than simultaneously.
const defaultMetatileTTL = 30 * time.Second

type MetatileConfig struct {
	// Width and height of the metatile in tiles. Must be a power of two.
	Size int
	// Seconds a fetched metatile is retained to serve its siblings. Negative disables retention,
	// leaving only in-flight coalescing.
	CacheTTLSeconds *int `mapstructure:"cacheTtlSeconds"`
	Provider        map[string]interface{}
}

type Metatile struct {
	MetatileConfig
	provider  layer.Provider
	zoomDelta int
	ttl       time.Duration
	group     singleflight.Group
	mu        sync.Mutex
	entries   map[string]*metatileEntry
}

type metatileEntry struct {
	img     *pkg.Image
	expires time.Time
}

func init() {
	layer.RegisterProvider(MetatileRegistration{})
}

type MetatileRegistration struct {
}

func (s MetatileRegistration) InitializeConfig() any {
	return MetatileConfig{}
}

func (s MetatileRegistration) Name() string {
	return "metatile"
}

// The metatile itself is passed through untouched apart from being split, so the data type is
// whatever the wrapped provider produces.
func (s MetatileRegistration) DataType(cfgAny any) config.DataType {
	cfg, ok := cfgAny.(MetatileConfig)
	if !ok {
		return config.DataTypeUnknown
	}

	return layer.ExtractDataType(cfg.Provider)
}

func (s MetatileRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(MetatileConfig)

	if cfg.Size < 1 || cfg.Size > maxMetatileSize {
		return nil, fmt.Errorf(deps.ErrorMessages.RangeError, "provider.metatile.size", 1, maxMetatileSize)
	}

	// The zoom delta is log2(size), so anything but a power of two has no whole zoom level to
	// fetch the metatile from.
	if bits.OnesCount(uint(cfg.Size)) != 1 {
		return nil, fmt.Errorf(deps.ErrorMessages.InvalidParam, "provider.metatile.size", strconv.Itoa(cfg.Size))
	}

	provider, err := layer.ConstructProvider(cfg.Provider, deps)
	if err != nil {
		return nil, err
	}

	ttl := defaultMetatileTTL
	if cfg.CacheTTLSeconds != nil {
		ttl = time.Duration(*cfg.CacheTTLSeconds) * time.Second
	}

	return &Metatile{
		MetatileConfig: cfg,
		provider:       provider,
		zoomDelta:      bits.TrailingZeros(uint(cfg.Size)),
		ttl:            ttl,
		entries:        make(map[string]*metatileEntry),
	}, nil
}

func (t *Metatile) PreAuth(ctx context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return t.provider.PreAuth(ctx, providerContext)
}

// metatileFor maps a requested tile onto the lower zoom tile covering it, along with the
// sub-tile's column and row within that metatile. Returns ok=false when the zoom delta would take
// the metatile above zoom 0, in which case there's nothing larger to fetch.
func metatileFor(req pkg.TileRequest, zoomDelta int) (meta pkg.TileRequest, col, row int, ok bool) {
	if zoomDelta <= 0 || req.Z-zoomDelta < 0 {
		return req, 0, 0, false
	}

	metaX := req.X >> zoomDelta
	metaY := req.Y >> zoomDelta
	mask := (1 << zoomDelta) - 1

	return pkg.TileRequest{
			LayerName: req.LayerName,
			Z:         req.Z - zoomDelta,
			X:         metaX,
			Y:         metaY,
		},
		req.X & mask,
		req.Y & mask,
		true
}

func (t *Metatile) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	// Validates the request coordinates before anything derives a metatile from them.
	if _, err := tileRequest.GetBounds(); err != nil {
		return nil, err
	}

	meta, col, row, ok := metatileFor(tileRequest, t.zoomDelta)
	if !ok {
		// Too close to zoom 0 to have a larger tile to split, so pass the request straight through.
		return t.provider.GenerateTile(ctx, providerContext, tileRequest)
	}

	img, err := t.fetchMetatile(ctx, providerContext, meta)
	if err != nil {
		return nil, err
	}

	return t.split(img, tileRequest, meta, col, row)
}

// fetchMetatile returns the metatile image, making at most one upstream call per metatile no
// matter how many sub-tiles ask for it concurrently.
func (t *Metatile) fetchMetatile(ctx context.Context, providerContext layer.ProviderContext, meta pkg.TileRequest) (*pkg.Image, error) {
	key := meta.String()

	if img, ok := t.cached(key); ok {
		return img, nil
	}

	// DoChan, not Do: whichever caller's goroutine happens to become the leader must not have its
	// own context cancellation abort the fetch out from under every other sub-tile still waiting on
	// it. The fetch itself runs on a context detached from any single caller (but still cancellable
	// if every waiter goes away - see below), while each caller here still honors its own ctx.Done()
	// independently by racing the shared result against it.
	resultCh := t.group.DoChan(key, func() (interface{}, error) {
		if img, ok := t.cached(key); ok {
			return img, nil
		}

		// context.WithoutCancel, not ctx directly: this goroutine outlives whichever single
		// caller happened to trigger it, since other sub-tiles are also waiting on the result.
		// Request-scoped values (if any) still flow through; only the deadline/cancellation is
		// detached.
		img, err := t.provider.GenerateTile(context.WithoutCancel(ctx), providerContext, meta)
		if err != nil {
			return nil, err
		}

		t.store(key, img)

		return img, nil
	})

	select {
	case res := <-resultCh:
		if res.Err != nil {
			return nil, res.Err
		}

		img, ok := res.Val.(*pkg.Image)
		if !ok || img == nil {
			return nil, fmt.Errorf("metatile: no image returned for %v", key)
		}

		return img, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *Metatile) cached(key string) (*pkg.Image, bool) {
	if t.ttl <= 0 {
		return nil, false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.entries[key]
	if !ok {
		return nil, false
	}

	if time.Now().After(entry.expires) {
		delete(t.entries, key)
		return nil, false
	}

	return entry.img, true
}

func (t *Metatile) store(key string, img *pkg.Image) {
	if t.ttl <= 0 || img.ForceSkipCache {
		return
	}

	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	// Expired entries are only dropped when something else is stored, which keeps the map from
	// growing without a background reaper.
	for k, e := range t.entries {
		if now.After(e.expires) {
			delete(t.entries, k)
		}
	}

	t.entries[key] = &metatileEntry{img: img, expires: now.Add(t.ttl)}
}

// split extracts the sub-tile at the given column and row from the metatile.
func (t *Metatile) split(img *pkg.Image, tileRequest, meta pkg.TileRequest, col, row int) (*pkg.Image, error) {
	if img.ContentType == mvtContentType {
		return t.splitVector(img, tileRequest, meta)
	}

	return t.splitRaster(img, col, row)
}

// splitRaster crops the sub-tile's pixels out of the larger image. The crop is scaled to the
// image that actually came back, so an upstream serving any size gets divided evenly.
func (t *Metatile) splitRaster(img *pkg.Image, col, row int) (*pkg.Image, error) {
	realImage, _, err := image.Decode(bytes.NewReader(img.Content))
	if err != nil {
		return nil, err
	}

	bounds := realImage.Bounds()
	subWidth := float64(bounds.Dx()) / float64(t.Size)
	subHeight := float64(bounds.Dy()) / float64(t.Size)

	if subWidth < 1 || subHeight < 1 {
		return nil, fmt.Errorf("metatile: image of %vx%v is too small to split into a %vx%v grid", bounds.Dx(), bounds.Dy(), t.Size, t.Size)
	}

	// Rounding each edge independently rather than multiplying a truncated size keeps the
	// sub-tiles covering the whole image when the dimensions don't divide evenly.
	x0 := bounds.Min.X + int(math.Round(float64(col)*subWidth))
	x1 := bounds.Min.X + int(math.Round(float64(col+1)*subWidth))
	y0 := bounds.Min.Y + int(math.Round(float64(row)*subHeight))
	y1 := bounds.Min.Y + int(math.Round(float64(row+1)*subHeight))

	sub := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			sub.Set(x-x0, y-y0, realImage.At(x, y))
		}
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	if err = png.Encode(writer, sub); err != nil {
		return nil, err
	}

	if err = writer.Flush(); err != nil {
		return nil, err
	}

	return &pkg.Image{Content: buf.Bytes(), ContentType: mimePng, ForceSkipCache: img.ForceSkipCache}, nil
}

// splitVector re-tiles the metatile's geometry onto the requested tile. Vector coordinates are
// relative to their own tile's extent, so the geometry is projected back out to WGS84 using the
// metatile's grid position and then re-projected against the sub-tile, which clips it too.
func (t *Metatile) splitVector(img *pkg.Image, tileRequest, meta pkg.TileRequest) (*pkg.Image, error) {
	if len(img.Content) == 0 {
		return &pkg.Image{Content: []byte{}, ContentType: mvtContentType, ForceSkipCache: img.ForceSkipCache}, nil
	}

	layers, err := mvt.Unmarshal(img.Content)
	if err != nil {
		return nil, err
	}

	//#nosec G115 -- both requests have already been range checked by GetBounds
	metaTile := maptile.New(uint32(meta.X), uint32(meta.Y), maptile.Zoom(meta.Z))
	//#nosec G115 -- both requests have already been range checked by GetBounds
	subTile := maptile.New(uint32(tileRequest.X), uint32(tileRequest.Y), maptile.Zoom(tileRequest.Z))

	layers.ProjectToWGS84(metaTile)
	layers.Clip(subTile.Bound())
	layers.ProjectToTile(subTile)

	output, err := mvt.Marshal(layers)
	if err != nil {
		return nil, err
	}

	return &pkg.Image{Content: output, ContentType: mvtContentType, ForceSkipCache: img.ForceSkipCache}, nil
}
