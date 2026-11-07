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

package layers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/datastores"
	"github.com/Michad/tilegroxy/internal/static"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var packageName = static.GetPackage()

var metricNameSafeChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// Leaves room for the "tilegroxy.tiles.layer." prefix and longest suffix within OTEL's 255 character limit
const maxSanitizedMetricNameLen = 200

// OTEL rejects other characters, fatally at startup. '.' and '/' are also replaced so an ID can't inject segments
func sanitizeMetricName(id string) string {
	sanitized := metricNameSafeChars.ReplaceAllString(id, "_")

	if len(sanitized) > maxSanitizedMetricNameLen {
		sanitized = sanitized[:maxSanitizedMetricNameLen]
	}

	return sanitized
}

type layerSegment struct {
	value       string
	placeholder bool
}

// Prepends while checking for duplicate segments and propagating errors
func prependLayerSegment(existingSegments []layerSegment, newSegment layerSegment, errs error) ([]layerSegment, error) {
	if newSegment.placeholder {
		if len(existingSegments) > 0 && existingSegments[0].placeholder {
			errs = errors.Join(errs, errors.New("placeholders without separators"))
		}

		for _, cur := range existingSegments {
			if cur.placeholder && newSegment.value == cur.value {
				errs = errors.Join(errs, errors.New("dupe: "+newSegment.value))
			}
		}
	}

	return slices.Concat([]layerSegment{newSegment}, existingSegments), errs
}

// Each segment is either a placeholder or a literal
func parsePattern(pattern string) ([]layerSegment, error) {
	if pattern == "" {
		return []layerSegment{}, nil
	}

	firstOpening := strings.Index(pattern, "{")
	firstClosing := strings.Index(pattern, "}")

	if firstOpening > 0 {
		seg := layerSegment{value: pattern[0:firstOpening], placeholder: false}
		next, err := parsePattern(pattern[firstOpening:])
		return prependLayerSegment(next, seg, err)
	} else if firstOpening == 0 {
		if firstClosing > 0 {
			seg := layerSegment{value: pattern[1:firstClosing], placeholder: true}
			next, err := parsePattern(pattern[firstClosing+1:])
			return prependLayerSegment(next, seg, err)
		}

		return []layerSegment{{value: pattern[1:], placeholder: true}}, errors.New("missing }")
	}

	return []layerSegment{{value: pattern, placeholder: false}}, nil
}

func match(segments []layerSegment, str string) (bool, map[string]string) {
	matches := make(map[string]string)
	var lastSeg *layerSegment
	strLoc := 0
	for _, seg := range segments {
		if seg.placeholder {
			lastSeg = &seg
		} else {
			matchLoc := strings.Index(str[strLoc:], seg.value)
			if matchLoc >= 0 {
				if lastSeg != nil {
					matches[lastSeg.value] = str[strLoc : matchLoc+strLoc]
				} else if matchLoc > 0 {
					return false, matches
				}
				strLoc = matchLoc + strLoc + len(seg.value)
			} else {
				return false, matches
			}
			lastSeg = nil
		}
	}
	if lastSeg != nil {
		matches[lastSeg.value] = str[strLoc:]
	} else if strLoc < len(str) {
		return false, matches
	}

	return true, matches
}

// Keys are matched to placeholders ignoring case since the config loader lowercases them
func placeholderName(segments []layerSegment, key string) string {
	for _, seg := range segments {
		if seg.placeholder && strings.EqualFold(seg.value, key) {
			return seg.value
		}
	}

	return ""
}

func constructValidation(raw map[string]string, segments []layerSegment, errorMessages config.ErrorMessages) (map[string]*regexp.Regexp, error) {
	if raw == nil {
		return nil, nil
	}

	res := make(map[string]*regexp.Regexp)
	errs := make([]error, 0)

	for k, v := range raw {
		var err error

		if v == "" {
			errs = append(errs, fmt.Errorf(errorMessages.InvalidParam, "layer.paramValidator."+k, v))
			continue
		}

		key := k
		if name := placeholderName(segments, k); name != "" {
			key = name
		}

		// Grouping makes the anchors apply to every alternative, not just the first and last
		res[key], err = regexp.Compile("^(?:" + v + ")$")

		if err != nil {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return res, nil
}

func validateParamMatches(values map[string]string, regexp map[string]*regexp.Regexp) bool {
	if regexp == nil {
		return true
	}

	for k, r := range regexp {
		if k == "*" {
			for _, v := range values {
				if !r.MatchString(v) {
					return false
				}
			}
		} else if !r.MatchString(values[k]) {
			return false
		}
	}

	return true
}

type paramMapping struct {
	values     map[string]string
	hasDefault bool
	defaultTo  string // Empty passes unlisted values through unchanged
}

func constructParamMap(raw map[string][]config.ParamMapEntry, segments []layerSegment, errorMessages config.ErrorMessages) (map[string]paramMapping, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	res := make(map[string]paramMapping, len(raw))
	errs := make([]error, 0)

	for k, entries := range raw {
		placeholder := placeholderName(segments, k)
		if placeholder == "" {
			errs = append(errs, fmt.Errorf(errorMessages.InvalidParam, "layer.paramMap", k))
			continue
		}

		mapping := paramMapping{values: make(map[string]string, len(entries))}
		for _, entry := range entries {
			if entry.Default {
				if mapping.hasDefault || entry.From != "" {
					errs = append(errs, fmt.Errorf(errorMessages.InvalidParam, "layer.paramMap."+k+".default", entry.From))
				}
				mapping.hasDefault = true
				mapping.defaultTo = entry.To
				continue
			}

			if _, dupe := mapping.values[entry.From]; dupe {
				errs = append(errs, fmt.Errorf(errorMessages.InvalidParam, "layer.paramMap."+k+".from", entry.From))
			}
			mapping.values[entry.From] = entry.To
		}

		res[placeholder] = mapping
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return res, nil
}

// Replaces values in place. False when a mapped placeholder's value isn't listed and there's no default
func applyParamMap(values map[string]string, paramMap map[string]paramMapping) bool {
	for k, mapping := range paramMap {
		if to, ok := mapping.values[values[k]]; ok {
			values[k] = to
		} else if !mapping.hasDefault {
			return false
		} else if mapping.defaultTo != "" {
			values[k] = mapping.defaultTo
		}
	}

	return true
}

func unprotectedPlaceholders(matching layerMatching) []string {
	res := make([]string, 0)

	for _, seg := range matching.segments {
		if !seg.placeholder {
			continue
		}

		if mapping, ok := matching.paramMap[seg.value]; ok && (!mapping.hasDefault || mapping.defaultTo != "") {
			continue
		}

		validated := false
		for k := range matching.validator {
			if k == "*" || k == seg.value {
				validated = true
			}
		}

		if !validated {
			res = append(res, seg.value)
		}
	}

	return res
}

func matchName(segments []layerSegment, validator map[string]*regexp.Regexp, paramMap map[string]paramMapping, layerName string) (bool, map[string]string) {
	doesMatch, matches := match(segments, layerName)
	if !doesMatch || !validateParamMatches(matches, validator) || !applyParamMap(matches, paramMap) {
		return false, nil
	}

	return true, matches
}

type Layer struct {
	ID                 string
	Pattern            []layerSegment
	ParamValidator     map[string]*regexp.Regexp
	ParamMap           map[string]paramMapping // Keyed by placeholder name
	Config             config.LayerConfig
	Provider           layer.Provider
	Cache              cache.Cache
	ErrorMessages      config.ErrorMessages
	DataType           config.DataType // The version from the config with auto-resolution applied
	providerContext    layer.ProviderContext
	authMutex          sync.Mutex
	allowCoalesce      bool              // The version from config with auto-resolution applied
	CacheControl       CacheControlFacts // What this layer's cache and provider say about caching its tiles downstream
	tileAllCounter     metric.Int64Counter
	tileAuthCounter    metric.Int64Counter
	tileErrorCounter   metric.Int64Counter
	tileSuccessCounter metric.Int64Counter
	metadata           ResolvedMetadata
}

// The inner provider is built and described first so the right bounds wrapper can be picked from its data type
func wrapBounds(inner layer.Provider, rawConfig config.LayerConfig, datatype config.DataType, deps layer.ProviderDeps) (layer.Provider, error) {
	wrapperName := "cropmvt"

	switch datatype {
	case config.DataTypeRaster:
		wrapperName = "crop"
	case config.DataTypeMLT:
		wrapperName = "cropmlt"
	case config.DataTypeMVT, config.DataTypeUnknown:
	}

	reg, ok := layer.RegisteredProvider(wrapperName)
	wrapper, isWrapper := reg.(layer.BoundsWrapper)
	if !ok || !isWrapper {
		return nil, fmt.Errorf(deps.ErrorMessages.EnumError, "provider.name", wrapperName, layer.RegisteredProviderNames())
	}

	p, err := wrapper.WrapBounds(inner, pkg.BoundsFromConfig(rawConfig.Bounds), deps)
	if err != nil {
		return nil, err
	}

	return ProviderWrapper{Name: wrapperName, Provider: p, dataType: datatype}, nil
}

// A provider config mentioning either produces per-identity tiles
var identityPlaceholders = []string{"{ctx.user}", "{ctx.tenant}"}

// Walks the whole tree since nesting providers hide configs and placeholders can sit in map keys as well as values
func usesIdentityPlaceholder(node any) bool {
	switch v := node.(type) {
	case string:
		return slices.ContainsFunc(identityPlaceholders, func(placeholder string) bool {
			return strings.Contains(v, placeholder)
		})
	case map[string]any:
		for key, val := range v {
			if usesIdentityPlaceholder(key) || usesIdentityPlaceholder(val) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(v, usesIdentityPlaceholder)
	}

	return false
}

// Every waiter gets the leader's tile, so only default on when the layer caches and nothing marks tiles as per-requester
func resolveAllowCoalesce(rawConfig config.LayerConfig, layerCache cache.Cache) bool {
	if rawConfig.AllowCoalesce != nil {
		return *rawConfig.AllowCoalesce
	}

	if rawConfig.SkipCache {
		return false
	}

	if caches.IsKeyedByIdentity(layerCache) {
		return false
	}

	if usesIdentityPlaceholder(rawConfig.Provider) {
		return false
	}

	return !caches.IsNoop(layerCache)
}

// What a layer's config says about downstream caching, resolved at construction. The server turns these into headers
type CacheControlFacts struct {
	// The layer keeps no tiles itself, which argues against downstream caches keeping them
	Uncacheable bool
	// Tiles appear to vary by requester, so a shared cache must not hand one caller's tile to the next
	PerIdentity bool
	// Zero means the layer's cache enforces no TTL
	TTL time.Duration
}

func resolveCacheControlFacts(rawConfig config.LayerConfig, layerCache cache.Cache) CacheControlFacts {
	facts := CacheControlFacts{
		Uncacheable: rawConfig.SkipCache || caches.IsNoop(layerCache),
		PerIdentity: caches.IsKeyedByIdentity(layerCache) || usesIdentityPlaceholder(rawConfig.Provider),
	}

	if !facts.Uncacheable {
		if ttl, ok := caches.ExtractTTLFromCache(layerCache); ok {
			facts.TTL = ttl
		}
	}

	return facts
}

type layerMatching struct {
	segments  []layerSegment
	validator map[string]*regexp.Regexp
	paramMap  map[string]paramMapping
}

func resolveMatching(rawConfig config.LayerConfig, errorMessages config.ErrorMessages) (layerMatching, error) {
	isPattern := rawConfig.Pattern != "" && rawConfig.Pattern != rawConfig.ID

	var res layerMatching
	var err error
	if isPattern {
		res.segments, err = parsePattern(rawConfig.Pattern)
		if err != nil {
			return res, fmt.Errorf(errorMessages.InvalidParam, "layer.pattern", rawConfig.Pattern)
		}
	} else {
		res.segments = []layerSegment{{value: rawConfig.ID, placeholder: false}}
	}

	if isPattern && rawConfig.ParamValidator != nil {
		res.validator, err = constructValidation(rawConfig.ParamValidator, res.segments, errorMessages)
		if err != nil {
			return res, err
		}
	}

	res.paramMap, err = constructParamMap(rawConfig.ParamMap, res.segments, errorMessages)
	if err != nil {
		return res, err
	}

	for _, example := range rawConfig.Examples {
		if !isPattern {
			return res, fmt.Errorf(errorMessages.InvalidParam, "layer.examples", example)
		}

		if doesMatch, _ := matchName(res.segments, res.validator, res.paramMap, example); !doesMatch {
			return res, fmt.Errorf(errorMessages.InvalidParam, "layer.examples", example)
		}
	}

	return res, nil
}

func constructLayerCounters(layerID string) (metric.Int64Counter, metric.Int64Counter, metric.Int64Counter, metric.Int64Counter, error) {
	meter := otel.Meter(packageName)
	sanitizedID := sanitizeMetricName(layerID)

	tileAllCounter, err1 := meter.Int64Counter("tilegroxy.tiles.layer."+sanitizedID+".request", metric.WithDescription("Number of tile requests for "+layerID))
	tileAuthCounter, err2 := meter.Int64Counter("tilegroxy.tiles.layer."+sanitizedID+".auth", metric.WithDescription("Number of outgoing authentication checks for "+layerID))
	tileErrorCounter, err3 := meter.Int64Counter("tilegroxy.tiles.layer."+sanitizedID+".error", metric.WithDescription("Number of tile requests that error during generation for "+layerID))
	tileSuccessCounter, err4 := meter.Int64Counter("tilegroxy.tiles.layer."+sanitizedID+".success", metric.WithDescription("Number of tile requests that result in a tile for "+layerID))

	return tileAllCounter, tileAuthCounter, tileErrorCounter, tileSuccessCounter, errors.Join(err1, err2, err3, err4)
}

func ConstructLayer(ctx context.Context, rawConfig config.LayerConfig, defaultClientConfig config.ClientConfig, layerCache cache.Cache, errorMessages config.ErrorMessages, layerGroup *LayerGroup, secreter secret.Secreter, datastoreRegistry *datastores.Registry, clientTransport func(config.ClientConfig) (http.RoundTripper, error)) (*Layer, error) {
	var err error
	if rawConfig.Client == nil {
		rawConfig.Client = &defaultClientConfig
	} else {
		configload.MergeClientDefaults(rawConfig.Client, defaultClientConfig)

	}

	var transport http.RoundTripper
	if clientTransport != nil {
		transport, err = clientTransport(*rawConfig.Client)
		if err != nil {
			return nil, err
		}
	}

	rawConfig.Provider = configload.ReplaceEnv(rawConfig.Provider)
	if secreter != nil {
		rawConfig.Provider, err = configload.ReplaceConfigValues(rawConfig.Provider, "secret", func(k string) (string, error) {
			v, _, lookupErr := secreter.Lookup(ctx, k)
			return v, lookupErr
		})
		if err != nil {
			return nil, err
		}
	}

	matching, err := resolveMatching(rawConfig, errorMessages)
	if err != nil {
		return nil, err
	}

	if unprotected := unprotectedPlaceholders(matching); len(unprotected) > 0 {
		slog.WarnContext(ctx, fmt.Sprintf("Layer %v accepts any value for placeholders %v. Restrict them with paramValidator or paramMap", rawConfig.ID, unprotected))
	}

	deps := layer.ProviderDeps{
		ClientConfig:  *rawConfig.Client,
		ErrorMessages: errorMessages,
		Transport:     transport,
		LayerGroup:    layerGroup,
		Datastores:    datastoreRegistry,
	}

	provider, err := ConstructProvider(rawConfig.Provider, deps)
	if err != nil {
		return nil, err
	}

	metadata, err := resolveMetadata(rawConfig.ID, rawConfig.LayerMetadata, layer.DescribeTree(provider), errorMessages)
	if err != nil {
		return nil, errors.Join(err, layer.CloseProvider(ctx, provider))
	}

	datatype := metadata.Advertised.DataType

	if rawConfig.Bounds != (config.BoundsConfig{}) {
		if !isKnownDataType(datatype) {
			return nil, errors.Join(fmt.Errorf(errorMessages.ParamRequired, "layer.datatype"), layer.CloseProvider(ctx, provider))
		}

		wrapped, err := wrapBounds(provider, rawConfig, datatype, deps)
		if err != nil {
			return nil, errors.Join(err, layer.CloseProvider(ctx, provider))
		}
		provider = wrapped
	}

	allowCoalesce := resolveAllowCoalesce(rawConfig, layerCache)

	tileAllCounter, tileAuthCounter, tileErrorCounter, tileSuccessCounter, err := constructLayerCounters(rawConfig.ID)
	if err != nil {
		return nil, errors.Join(err, layer.CloseProvider(ctx, provider))
	}

	return &Layer{
		ID:                 rawConfig.ID,
		Pattern:            matching.segments,
		ParamValidator:     matching.validator,
		ParamMap:           matching.paramMap,
		Config:             rawConfig,
		Provider:           provider,
		Cache:              layerCache,
		ErrorMessages:      errorMessages,
		DataType:           datatype,
		allowCoalesce:      allowCoalesce,
		CacheControl:       resolveCacheControlFacts(rawConfig, layerCache),
		tileAllCounter:     tileAllCounter,
		tileAuthCounter:    tileAuthCounter,
		tileErrorCounter:   tileErrorCounter,
		tileSuccessCounter: tileSuccessCounter,
		metadata:           metadata,
	}, nil
}

// Re-authenticates first if needed. The mutex spans the read-check-write so concurrent requests never see a torn or stale value
func (l *Layer) getProviderContext(ctx context.Context) (layer.ProviderContext, error) {
	var err error

	l.authMutex.Lock()
	defer l.authMutex.Unlock()

	if !l.providerContext.AuthBypass && l.providerContext.AuthExpiration.Before(time.Now()) {
		l.tileAuthCounter.Add(ctx, 1)
		l.providerContext, err = l.Provider.PreAuth(ctx, l.providerContext)
	}

	return l.providerContext, err
}

// Discards the expiration so the next getProviderContext re-authenticates
func (l *Layer) forceReauth(ctx context.Context) (layer.ProviderContext, error) {
	l.authMutex.Lock()
	l.providerContext.AuthExpiration = time.Time{}
	l.authMutex.Unlock()

	return l.getProviderContext(ctx)
}

func (l *Layer) MatchesName(ctx context.Context, layerName string) bool {
	doesMatch, matches := matchName(l.Pattern, l.ParamValidator, l.ParamMap, layerName)
	if !doesMatch {
		return false
	}

	layerPatternMatches, _ := pkg.LayerPatternMatchesFromContext(ctx)
	if layerPatternMatches != nil {
		*layerPatternMatches = matches
	}

	return true
}

// For callers working from raw config before layers are built. An unparseable pattern matches nothing; construction reports it
func ConfigMatchesName(rawConfig config.LayerConfig, errorMessages config.ErrorMessages, layerName string) bool {
	matching, err := resolveMatching(rawConfig, errorMessages)
	if err != nil {
		return false
	}

	doesMatch, _ := matchName(matching.segments, matching.validator, matching.paramMap, layerName)

	return doesMatch
}

// True when a pattern distinct from the ID means there's no single tile URL, so TileJSON needs Config.Examples
func (l *Layer) IsPattern() bool {
	return l.Config.Pattern != "" && l.Config.Pattern != l.Config.ID
}

// Called before the cache lookup so a cached tile can't bypass a zoom limit added later
func (l *Layer) CheckZoomBounds(tileRequest pkg.TileRequest) error {
	minZoom, maxZoom := zoomRange(l.metadata.Limits.MinZoom, l.metadata.Limits.MaxZoom)

	if tileRequest.Z < minZoom || tileRequest.Z > maxZoom {
		return pkg.RangeError{ParamName: "z", MinValue: float64(minZoom), MaxValue: float64(maxZoom)}
	}

	return nil
}

func (l *Layer) Metadata() ResolvedMetadata {
	return l.metadata
}

func (l *Layer) RenderTileNoCache(ctx context.Context, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	var img *pkg.Image
	var err error

	if err := l.CheckZoomBounds(tileRequest); err != nil {
		return nil, err
	}

	l.tileAllCounter.Add(ctx, 1)

	providerContext, err := l.getProviderContext(ctx)

	if err != nil {
		return nil, err
	}

	img, err = l.Provider.GenerateTile(ctx, providerContext, tileRequest)

	var authError pkg.ProviderAuthError
	if errors.As(err, &authError) {
		providerContext, err = l.forceReauth(ctx)

		if err != nil {
			return nil, err
		}

		img, err = l.Provider.GenerateTile(ctx, providerContext, tileRequest)

		if err != nil {
			l.tileErrorCounter.Add(ctx, 1)
			return nil, err
		}
	} else if err != nil {
		l.tileErrorCounter.Add(ctx, 1)
		return nil, err
	}

	l.tileSuccessCounter.Add(ctx, 1)
	return img, nil
}
