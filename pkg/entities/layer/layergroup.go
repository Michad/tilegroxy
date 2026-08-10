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
	"slices"
	"strings"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
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
	renderGroup       singleflight.Group
	cacheWriteLimiter chan struct{}
	// noCoalesceLayers holds the IDs of layers whose rendered output depends on request-scoped
	// context values (see requestScopedLayers). Coalescing is skipped for those layers.
	noCoalesceLayers map[string]bool
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

	// requestScopedLayers has to inspect resolved provider configs, since a {ctx.*} placeholder can
	// arrive through an env var or secret and is invisible in the raw config.
	resolvedLayers := make([]config.LayerConfig, len(cfg.Layers))

	for i, l := range cfg.Layers {
		resolvedLayers[i], err = resolveLayerProviderValues(l, secreter)
		if err != nil {
			return nil, fmt.Errorf("error constructing layer %v: %w", i, err)
		}

		layerObjects[i], err = ConstructLayer(resolvedLayers[i], cfg.Client, cfg.Error.Messages, &layerGroup, secreter, datastores)
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
	layerGroup.noCoalesceLayers = requestScopedLayers(resolvedLayers)

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

// containsRequestScopedPlaceholder reports whether any string anywhere in a raw provider config
// contains a `{ctx.` placeholder. Those resolve against the rendering context at request time,
// which holds every header of the incoming request, so a provider using one produces per-caller
// output.
func containsRequestScopedPlaceholder(node any) bool {
	switch v := node.(type) {
	case string:
		return strings.Contains(v, "{ctx.")
	case map[string]any:
		for _, val := range v {
			if containsRequestScopedPlaceholder(val) {
				return true
			}
		}
	case []any:
		for _, val := range v {
			if containsRequestScopedPlaceholder(val) {
				return true
			}
		}
	}
	return false
}

// resolveRefTargetsToIDs maps ref targets, which are layer *names* as they'd arrive in a request,
// onto the IDs of the layers that could serve them. requestScopedLayers is keyed by ID, so
// comparing raw targets against IDs would drop every edge into a pattern layer: a layer with id
// "secret" and pattern "secret-{v}" is reachable by the name "secret-foo".
//
// Resolution mirrors FindLayer, except that every candidate is returned rather than the first,
// since which one FindLayer picks can depend on the request, and ParamValidator is not consulted,
// since it can only reject a name. Both choices over-approximate deliberately.
//
// A target that resolves to nothing is dropped: an edge to a layer that doesn't exist can't carry
// a mark. validateRefs handles statically dangling targets.
func resolveRefTargetsToIDs(targets []string, layers []config.LayerConfig) []string {
	if len(targets) == 0 {
		return nil
	}

	ids := make([]string, 0, len(targets))
	seen := make(map[string]bool, len(targets))

	for _, target := range targets {
		for _, l := range layers {
			if seen[l.ID] {
				continue
			}

			isPattern := l.Pattern != "" && l.Pattern != l.ID
			if !isPattern {
				if l.ID != target {
					continue
				}
			} else {
				segments, err := parsePattern(l.Pattern)
				if err != nil {
					// ConstructLayer already errors on an unparseable pattern before this runs.
					continue
				}
				if doesMatch, _ := match(segments, target); !doesMatch {
					continue
				}
			}

			seen[l.ID] = true
			ids = append(ids, l.ID)
		}
	}

	return ids
}

// requestScopedLayers returns the set of layer IDs for which coalescing must be disabled because
// their rendered content can differ between two callers asking for the same layer name and tile
// coordinates.
//
// Coalescing hands the leader's tile to every waiter, which is only sound when the render is a
// pure function of the singleflight key. Of the inputs a render can draw on, only {ctx.*} is
// request-scoped and therefore unsafe: it resolves against the rendering context, which carries
// the leader's HTTP headers, so a waiter would receive a tile fetched with the leader's
// credentials. {layer.*} resolves from the pattern matches on the layer name, which is part of the
// key. {env.*}, config, and secrets are process-scoped. Authorization runs per-caller in
// checkPermission before the singleflight Do and doesn't feed the rendered bytes.
//
// The ref provider rebuilds the child context from the original request, so headers propagate
// across a ref and a layer that refs an unsafe layer is itself unsafe. The marking is propagated
// transitively below.
//
// Coalescing is an optimization, so over-approximating here costs only a duplicate upstream fetch
// while under-approximating is a cross-user disclosure.
func requestScopedLayers(layers []config.LayerConfig) map[string]bool {
	unsafeLayers := make(map[string]bool)
	refsByLayer := make(map[string][]string, len(layers))

	for _, l := range layers {
		if containsRequestScopedPlaceholder(l.Provider) {
			unsafeLayers[l.ID] = true
		}

		var targets []string
		findRefTargets(l.Provider, &targets)
		refsByLayer[l.ID] = resolveRefTargetsToIDs(targets, layers)
	}

	// Bounded by the layer count: each pass either marks another layer or stops.
	for changed := true; changed; {
		changed = false
		for id, targets := range refsByLayer {
			if unsafeLayers[id] {
				continue
			}
			for _, target := range targets {
				if unsafeLayers[target] {
					unsafeLayers[id] = true
					changed = true
					break
				}
			}
		}
	}

	return unsafeLayers
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

	img, err = lg.renderCoalesced(ctx, l, tileRequest)

	if err != nil {
		return nil, err
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

// renderCoalesced renders a tile, collapsing concurrent misses for the same tile into a single
// upstream render where that's safe.
//
// Callers must have already run checkPermission for their own context, so that a caller can never
// ride along on a render they weren't permitted to request. Layers whose output depends on
// request-scoped values (see requestScopedLayers) skip coalescing and render under the caller's
// own context.
func (lg *LayerGroup) renderCoalesced(ctx context.Context, l *Layer, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	if lg.noCoalesceLayers[l.ID] {
		img, err := lg.RenderTileNoCache(ctx, tileRequest)
		if err != nil {
			return nil, err
		}
		if img == nil {
			return nil, errNilImage
		}
		return img, nil
	}

	sfKey := tileRequest.LayerName + "/" + tileRequest.String()

	imgAny, err, _ := lg.renderGroup.Do(sfKey, func() (any, error) {
		// Detached from the leader's cancellation, which every waiter shares: otherwise the
		// leader disconnecting fails all the followers whose own requests are still alive. The
		// tradeoff is that a coalesced render runs to completion even once every waiter has gone
		// away, bounded only by the provider's HTTP timeouts. That's the same lifetime the
		// background cache write has, and better than one caller cancelling another's request.
		return lg.RenderTileNoCache(context.WithoutCancel(ctx), tileRequest)
	})

	if err != nil {
		return nil, err
	}

	img, ok := imgAny.(*pkg.Image)
	if !ok || img == nil {
		return nil, errNilImage
	}

	// Every waiter gets the identical pointer and callers do mutate the result in place, e.g.
	// fallback.go setting ForceSkipCache, so hand out a shallow copy to keep those scalar writes
	// private. Content stays shared since it's only ever read or copied out of.
	c := *img
	return &c, nil
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
