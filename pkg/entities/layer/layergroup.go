// Copyright 2024 Michael Davis
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

package layer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

// Bounds the background writeCache goroutines. A slow cache backend plus sustained misses would
// otherwise accumulate goroutines and the images they pin without limit.
const maxConcurrentCacheWrites = 64

type LayerGroup struct {
	layers            []*Layer
	DefaultCache      cache.Cache
	cacheHitCounter   metric.Int64Counter
	cacheMissCounter  metric.Int64Counter
	cacheWriteLimiter chan struct{}
	// generateGroup coalesces concurrent provider fetches for the same tile on a cache miss, so a
	// burst of requests for one cold tile results in a single upstream call instead of one per
	// request. Keyed identically to the cache key (see tileRequest.String()) so it never conflates
	// genuinely different tiles.
	generateGroup singleflight.Group
}

func ConstructLayerGroup(cfg config.Config, cache cache.Cache, secreter secret.Secreter, datastores *datastore.DatastoreRegistry) (*LayerGroup, error) {
	var err, err1, err2 error
	var layerGroup LayerGroup
	layerObjects := make([]*Layer, len(cfg.Layers))

	if err := validateNoDuplicateLayerIDs(cfg.Layers); err != nil {
		return nil, err
	}

	if err := validateRefs(cfg.Layers); err != nil {
		return nil, err
	}

	for i, l := range cfg.Layers {
		layerObjects[i], err = ConstructLayer(l, cfg.Client, cfg.Error.Messages, &layerGroup, secreter, datastores)
		if err != nil {
			return nil, fmt.Errorf("error constructing layer %v: %w", i, err)
		}

		layerObjects[i].Cache = cache
	}

	meter := otel.Meter(packageName)
	layerGroup.cacheHitCounter, err1 = meter.Int64Counter("tilegroxy.cache.total.hit", metric.WithDescription("Number of requests that hit the cache (ignoring skips)"))
	layerGroup.cacheMissCounter, err2 = meter.Int64Counter("tilegroxy.cache.total.miss", metric.WithDescription("Number of requests that missed the cache (ignoring skips)"))

	layerGroup.layers = layerObjects
	layerGroup.DefaultCache = cache
	layerGroup.cacheWriteLimiter = make(chan struct{}, maxConcurrentCacheWrites)

	return &layerGroup, errors.Join(err1, err2)
}

// findRefTargets recursively walks a raw provider config collecting the layer names that `ref`
// entries target. Providers like `blend` and `fallback` nest other providers inside themselves, so
// this can't be limited to the top level.
func findRefTargets(node any, targets *[]string) {
	switch v := node.(type) {
	case map[string]any:
		if name, ok := v["name"].(string); ok && name == "ref" {
			if target, ok := v["layer"].(string); ok && target != "" {
				*targets = append(*targets, target)
			}
		}
		for _, val := range v {
			findRefTargets(val, targets)
		}
	case []any:
		for _, val := range v {
			findRefTargets(val, targets)
		}
	}
}

// validateRefs errors on refs pointing at a layer ID that doesn't statically exist, and on cycles
// formed by refs. Only literal-ID layers can be resolved statically; refs targeting a patterned
// layer name are guarded at request time by the depth counter in Ref.GenerateTile instead.
func validateRefs(layers []config.LayerConfig) error {
	knownIDs := make(map[string]bool, len(layers))
	hasPatternLayer := false
	for _, l := range layers {
		if l.Pattern == "" || l.Pattern == l.ID {
			knownIDs[l.ID] = true
		} else {
			hasPatternLayer = true
		}
	}

	refsByLayer := make(map[string][]string, len(layers))
	for _, l := range layers {
		var targets []string
		findRefTargets(l.Provider, &targets)
		if len(targets) > 0 {
			refsByLayer[l.ID] = targets
		}
	}

	// With any pattern layer present, an unmatched target might still resolve to it at request
	// time, so a dangling target can only be flagged when every layer is literal-ID-matched.
	if !hasPatternLayer {
		for id, targets := range refsByLayer {
			for _, target := range targets {
				if !knownIDs[target] {
					return fmt.Errorf("layer %q has a ref provider targeting unknown layer %q", id, target)
				}
			}
		}
	}

	// DFS cycle detection over the ref graph (literal-ID-resolvable edges only)
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(refsByLayer))
	var path []string

	var visit func(id string) error
	visit = func(id string) error {
		switch color[id] {
		case gray:
			cycle := append(append([]string{}, path...), id)
			return fmt.Errorf("ref cycle detected among layers: %v", cycle)
		case black:
			return nil
		}

		color[id] = gray
		path = append(path, id)

		for _, target := range refsByLayer[id] {
			if _, ok := refsByLayer[target]; ok {
				if err := visit(target); err != nil {
					return err
				}
			}
		}

		path = path[:len(path)-1]
		color[id] = black
		return nil
	}

	for id := range refsByLayer {
		if color[id] == white {
			if err := visit(id); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateNoDuplicateLayerIDs errors if two layers share the same literal ID. Without this,
// FindLayer's linear scan silently makes the first match win and the second layer with that ID
// permanently unreachable - a config that "validates" but quietly drops a layer.
func validateNoDuplicateLayerIDs(layers []config.LayerConfig) error {
	seen := make(map[string]bool, len(layers))
	for _, l := range layers {
		if seen[l.ID] {
			return fmt.Errorf("duplicate layer id %q: every layer must have a unique id", l.ID)
		}
		seen[l.ID] = true
	}
	return nil
}

func (lg *LayerGroup) FindLayer(ctx context.Context, layerName string) *Layer {
	for _, l := range lg.layers {
		if l.MatchesName(ctx, layerName) {
			return l
		}
	}

	return nil
}

// Layers returns every configured layer, for callers that need to enumerate them rather than
// look one up by name, such as building the TileJSON index.
func (lg *LayerGroup) Layers() []*Layer {
	return lg.layers
}

func (lg *LayerGroup) ListLayerIDs() []string {
	r := make([]string, 0, len(lg.layers))
	for _, l := range lg.layers {
		r = append(r, l.ID)
	}
	return r
}

func (lg *LayerGroup) RenderTile(ctx context.Context, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	var img *pkg.Image
	var err error

	l := lg.FindLayer(ctx, tileRequest.LayerName)

	if l == nil {
		return nil, pkg.UnauthorizedError{Message: "Layer " + tileRequest.LayerName + " does not exist"}
	}

	if err := l.CheckZoomBounds(tileRequest); err != nil {
		return nil, err
	}

	if l.Config.SkipCache {
		return lg.RenderTileNoCache(ctx, tileRequest)
	}

	err = lg.checkPermission(ctx, l, tileRequest)
	if err != nil {
		return nil, err
	}

	img, err = l.Cache.Lookup(ctx, tileRequest)

	if img != nil {
		slog.DebugContext(ctx, "Cache hit")
		lg.cacheHitCounter.Add(ctx, 1)
		return img, err
	}

	lg.cacheMissCounter.Add(ctx, 1)

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Cache read error %v\n", err))
	}

	img, err = lg.renderTileNoCacheCoalesced(ctx, tileRequest)

	if err != nil {
		return nil, err
	}

	if img == nil {
		return nil, errNilImage
	}

	if !img.ForceSkipCache {
		select {
		case lg.cacheWriteLimiter <- struct{}{}:
			go func() {
				defer func() { <-lg.cacheWriteLimiter }()
				writeCache(ctx, l.Cache, tileRequest, img)
			}()
		default:
			slog.WarnContext(ctx, "Skipping cache write: too many cache writes already in flight")
		}
	}

	return img, nil
}

// errNilImage is returned when a provider reports success but hands back no image. composite_mvt
// and blend already defend against nested providers doing this, so it's reachable in practice.
var errNilImage = errors.New("provider returned no image and no error")

// singleflightResult bundles what a coalesced generation call produces, so it can travel through
// singleflight's `any` result value without a type assertion at every call site.
type singleflightResult struct {
	img   *pkg.Image
	err   error
	panic any // non-nil if the provider call underneath panicked instead of returning
	stack []byte
}

// renderTileNoCacheCoalesced deduplicates concurrent provider fetches for the same tile. Only one
// caller per key actually invokes RenderTileNoCache (the "leader"); the rest ("waiters") block on
// its result instead of each hitting the provider independently.
//
// The key matches the cache key exactly (tileRequest.String(), the same identity every Cache
// implementation keys on - see internal/caches) so this never serializes requests for genuinely
// different tiles.
//
// singleflight.Group deletes its map entry for a key as soon as the leader's call returns, before
// the result is delivered to any waiter, so a failed fetch is never "cached" - the very next
// caller (leader or waiter, whichever asks first afterward) always triggers a fresh attempt rather
// than replaying a stale error. See golang.org/x/sync/singleflight's Do/doCall.
//
// Each waiter selects between the shared result and its own ctx.Done(), so an individual caller's
// timeout can still fire while the leader keeps fetching for everyone else; it doesn't cancel the
// leader or any other waiter.
//
// A panic from the provider is deliberately recovered inside the shared call and re-thrown here,
// on the calling goroutine, instead of being left to singleflight's own panic handling. Otherwise
// every panic - even with just one caller and no real fan-out - would surface via the internal
// goroutine DoChan spawns to run the call, defeating a caller's own recover() (e.g. seedThread's)
// which only guards its own goroutine.
func (lg *LayerGroup) renderTileNoCacheCoalesced(ctx context.Context, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	key := tileRequest.String()

	// The leader's fetch must outlive any single caller's context, including its own, since
	// waiters with a longer deadline still need the leader to keep running after the original
	// caller's context is gone (cancelled by ctx.Done() below, or simply returned already).
	// context.WithoutCancel keeps every request-scoped value reachable from ctx - user ID, request
	// headers, layer permission restrictions, span - so a provider's {ctx.*} template placeholders
	// and similar still resolve correctly; it only detaches the deadline/cancellation, which is what
	// actually needs to outlive this one caller. A fresh pkg.BackgroundContext() would silently drop
	// all of that request-scoped data instead.
	leaderCtx := context.WithoutCancel(ctx)

	resultCh := lg.generateGroup.DoChan(key, func() (result any, _ error) {
		defer func() {
			if r := recover(); r != nil {
				result = singleflightResult{panic: r, stack: debug.Stack()}
			}
		}()

		img, err := lg.RenderTileNoCache(leaderCtx, tileRequest)
		return singleflightResult{img: img, err: err}, nil
	})

	select {
	case res := <-resultCh:
		// fn above never returns a non-nil error itself; res.Err always comes back nil here, and
		// the real result/error/panic is carried inside singleflightResult.
		sr, _ := res.Val.(singleflightResult)
		if sr.panic != nil {
			panic(fmt.Sprintf("%v\n\n%s", sr.panic, sr.stack))
		}
		return sr.img, sr.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func writeCache(ctx context.Context, cache cache.Cache, tileRequest pkg.TileRequest, img *pkg.Image) {
	// We need to make a new context to avoid the request finishing cancelling the ctx sent into the cache
	newCtx := pkg.BackgroundContext()

	// Copy span over from original context
	span := trace.SpanFromContext(ctx)
	newCtx = trace.ContextWithSpan(newCtx, span)

	// This runs on its own goroutine, so a panic from a third-party Cache.Save would otherwise be
	// unrecoverable and take down the process over a write no client is waiting on.
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(newCtx, fmt.Sprintf("Recovered from panic in background cache write: %v", r))
		}
	}()

	err := cache.Save(newCtx, tileRequest, img)

	if err != nil {
		slog.WarnContext(newCtx, fmt.Sprintf("Cache save error %v\n", err))
	}
}

func (*LayerGroup) checkPermission(ctx context.Context, l *Layer, tileRequest pkg.TileRequest) error {
	// These only return a usable pointer for a context built by pkg.NewRequestContext or
	// pkg.BackgroundContext. A library consumer can pass a plain stdlib context, so fall back to
	// unrestricted, which is what NewRequestContext installs anyway.
	ctxLimitLayers, ok := pkg.LimitLayersFromContext(ctx)
	limitLayers := ok && ctxLimitLayers != nil && *ctxLimitLayers

	ctxAllowedLayers, ok := pkg.AllowedLayersFromContext(ctx)
	var allowedLayers []string
	if ok && ctxAllowedLayers != nil {
		allowedLayers = *ctxAllowedLayers
	}

	ctxAllowedArea, ok := pkg.AllowedAreaFromContext(ctx)
	allowedArea := pkg.Bounds{}
	if ok && ctxAllowedArea != nil {
		allowedArea = *ctxAllowedArea
	}

	ctxLimitAreaPartial, ok := pkg.LimitAreaPartialFromContext(ctx)
	limitAreaPartial := ok && ctxLimitAreaPartial != nil && *ctxLimitAreaPartial

	if limitLayers {
		if !slices.Contains(allowedLayers, l.ID) {
			return pkg.UnauthorizedError{Message: "Denying access to non-allowed layer"}
		}
	}

	if !allowedArea.IsNullIsland() {
		bounds, err := tileRequest.GetBounds()
		if limitAreaPartial {
			if err != nil || !allowedArea.Intersects(*bounds) {
				return pkg.UnauthorizedError{Message: "Denying access to non-allowed area"}
			}
		} else {
			if err != nil || !allowedArea.Contains(*bounds) {
				return pkg.UnauthorizedError{Message: "Denying access to non-allowed area"}
			}
		}
	}
	return nil
}

// Close releases any layer provider holding resources, most notably custom providers whose operator
// script defines a close hook. Providers
// aren't required to implement Closer, so most layers are a no-op. Nesting providers (blend,
// fallback) forward to their own child providers via their own Close methods; ref deliberately
// doesn't, since it references another layer by name rather than owning a provider.
func (lg *LayerGroup) Close(ctx context.Context) error {
	if lg == nil {
		return nil
	}

	errs := make([]error, 0, len(lg.layers))

	for _, l := range lg.layers {
		if l == nil {
			continue
		}

		errs = append(errs, lifecycle.CloseIfCloser(ctx, l.Provider))
	}

	return errors.Join(errs...)
}

func (lg *LayerGroup) RenderTileNoCache(ctx context.Context, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	var err error

	l := lg.FindLayer(ctx, tileRequest.LayerName)

	if l == nil {
		return nil, pkg.UnauthorizedError{Message: "Layer " + tileRequest.LayerName + " does not exist"}
	}

	err = lg.checkPermission(ctx, l, tileRequest)
	if err != nil {
		return nil, err
	}

	return l.RenderTileNoCache(ctx, tileRequest)
}
