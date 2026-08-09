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

// maxConcurrentCacheWrites bounds how many background `go writeCache(...)` goroutines can be in
// flight at once. Without a bound, a slow cache backend plus sustained misses accumulates
// goroutines and the *pkg.Image buffers they pin without limit.
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
	layerGroup.noCoalesceLayers = requestScopedLayers(cfg.Layers)

	return &layerGroup, errors.Join(err1, err2)
}

// findRefTargets recursively walks a raw provider config (as parsed from YAML/JSON into
// map[string]any / []any) looking for `ref` provider entries, collecting the layer names
// they target. Providers like `blend` and `fallback` nest other provider configs inside
// themselves, so this can't be limited to the top level.
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
// (as parsed from YAML/JSON into map[string]any / []any) contains a `{ctx.` placeholder.
// replacePlaceholdersInString in the providers package resolves those against the *rendering*
// context at request time, which for a server request means any HTTP header (NewRequestContext
// puts every header into the context). A provider URL/body using e.g. {ctx.Authorization} or
// {ctx.X-Api-Key} therefore produces per-caller output.
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

// requestScopedLayers returns the set of layer IDs for which request coalescing (singleflight in
// RenderTile) must be disabled because their rendered content can differ between two callers
// asking for the same layer name and tile coordinates.
//
// Coalescing hands one caller's rendered tile to every other caller waiting on the same key. That
// is only sound when the render is a pure function of the key. The key covers the layer name and
// the tile coordinates, so the request-scoped inputs that need considering are:
//
//   - {ctx.*} placeholders: resolved from the rendering context, which carries every HTTP header
//     of whichever caller happened to become the singleflight leader. Serving that result to a
//     different caller discloses a tile fetched with the leader's credentials. Unsafe - detected
//     here.
//   - {layer.*} placeholders: resolved from pkg.LayerPatternMatchesFromContext, which is populated
//     by Layer.MatchesName from the requested layer name. Two callers sharing a singleflight key
//     share the same tileRequest.LayerName, so they necessarily produce identical pattern matches.
//     Safe - already keyed by the layer name being part of the key.
//   - {env.*} placeholders and config/secret values: process-scoped, identical for all callers.
//     Safe.
//   - Authorization inputs (allowed layers/areas, user ID): layer-level authorization runs
//     per-caller via checkPermission *before* the singleflight Do, so a caller never reaches a
//     shared render they weren't permitted to make. Those values don't otherwise feed the
//     rendered bytes.
//
// The `ref` provider forwards a request to another layer, rebuilding the child context from the
// original *http.Request (see internal/providers/ref.go), so headers propagate across a ref. A
// layer that refs an unsafe layer is therefore itself unsafe, and the marking below is propagated
// transitively across ref edges to a fixed point.
//
// Coalescing is a pure optimization, so declining it is always correct; false positives cost only
// a duplicate upstream fetch, whereas a false negative is a cross-user information disclosure.
func requestScopedLayers(layers []config.LayerConfig) map[string]bool {
	unsafeLayers := make(map[string]bool)
	refsByLayer := make(map[string][]string, len(layers))

	for _, l := range layers {
		if containsRequestScopedPlaceholder(l.Provider) {
			unsafeLayers[l.ID] = true
		}

		var targets []string
		findRefTargets(l.Provider, &targets)
		refsByLayer[l.ID] = targets
	}

	// Propagate across ref edges until nothing new is marked. Bounded by the number of layers
	// since each pass either marks at least one additional layer or stops.
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

// validateRefs performs startup validation of `ref` provider targets: it errors on refs that
// point at a layer ID that doesn't statically exist, and on cycles formed by refs. Only layers
// matched by a literal ID (no pattern) can be resolved statically; refs targeting a patterned
// layer name are skipped here and are instead guarded at request time by a depth counter in
// Ref.GenerateTile, since patterned layer names aren't all statically resolvable.
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

	// A dangling target can only be flagged with confidence when every layer is
	// literal-ID-matched; if any layer uses a pattern, an unmatched target might still
	// resolve to it at request time and we can't tell statically.
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

// errNilImage is returned when a provider reports success but hands back no image. Several
// providers (composite_mvt, blend) already defend against a nested provider returning (nil, nil),
// so it's a reachable state rather than a theoretical one; returning an error beats dereferencing
// nil and panicking on the caller's goroutine.
var errNilImage = errors.New("provider returned no image and no error")

// renderCoalesced renders a tile, collapsing concurrent misses for the same tile into a single
// upstream render where that's safe, so a burst of requests for one tile doesn't turn into N
// calls to the provider.
//
// Callers must have already run checkPermission for their own context - coalescing deliberately
// happens after authorization so a caller can never ride along on a render they weren't
// permitted to request.
//
// Layers whose output depends on request-scoped context values (see requestScopedLayers) skip
// coalescing entirely and render directly under the caller's own context, since handing them a
// leader's result would serve content fetched under a different user's identity.
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
		// Render under a context detached from any single caller's cancellation. The leader's
		// ctx is shared by every waiter, so tying the render to it means the leader
		// disconnecting fails all the followers too, even though their own requests are still
		// alive. detachedRenderContext keeps the values the render path needs (trace span,
		// authorization defaults, layer pattern matches) while dropping the leader's Done
		// channel and deadline, via context.WithoutCancel.
		//
		// Note the tradeoff this leaves: because the render no longer observes any caller's
		// cancellation, a coalesced render runs to completion (bounded by the provider's own
		// HTTP client timeouts) even if every waiter has gone away. That is the same lifetime
		// the background cache write already has, and it's preferable to the alternative of one
		// caller being able to cancel other callers' requests.
		return lg.RenderTileNoCache(context.WithoutCancel(ctx), tileRequest)
	})

	if err != nil {
		return nil, err
	}

	img, ok := imgAny.(*pkg.Image)
	if !ok || img == nil {
		return nil, errNilImage
	}

	// Every waiter on a singleflight key receives the identical pointer, and callers do mutate
	// the returned Image in place - internal/providers/fallback.go sets ForceSkipCache on an
	// image that may have come back through Ref.GenerateTile -> RenderTile. Handing out a shallow
	// per-caller copy keeps those scalar writes private. The Content slice stays shared: it's
	// only ever read or copied out of (composite_mvt builds a new slice via slices.Concat), never
	// appended to or written through in place.
	c := *img
	return &c, nil
}

func writeCache(ctx context.Context, cache cache.Cache, tileRequest pkg.TileRequest, img *pkg.Image) {
	// We need to make a new context to avoid the request finishing cancelling the ctx sent into the cache
	newCtx := pkg.BackgroundContext()

	// Copy span over from original context
	span := trace.SpanFromContext(ctx)
	newCtx = trace.ContextWithSpan(newCtx, span)

	// This runs on its own goroutine (see the `go writeCache(...)` call site) detached from any
	// request, so a panic here - e.g. from a buggy third-party Cache.Save implementation - would
	// otherwise be unrecoverable and take down the whole process for a background write that a
	// client isn't even waiting on. Logs against newCtx for the same reason the save below does:
	// the request ctx may already be cancelled by the time this fires.
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
	// LimitLayersFromContext et al only return a usable pointer when the context was built via
	// pkg.NewRequestContext/pkg.BackgroundContext. A library consumer calling RenderTile with a
	// plain context.Background() (a stdlib context, not one of ours) used to dereference these
	// nil pointers and panic; default to "unrestricted" instead, matching what NewRequestContext
	// itself installs as the zero-value default.
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
