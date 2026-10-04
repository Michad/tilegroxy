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
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/go-viper/mapstructure/v2"
)

// Anything outside this set is rejected at startup so a typo doesn't silently produce a column of nulls
const (
	FieldLayerName   = "layername"
	FieldLayerParams = "layerparams"
	FieldTenantID    = "tenantid"
	FieldIP          = "ip"
	FieldUserAgent   = "useragent"
	FieldReferer     = "referer"
	FieldHost        = "host"
	FieldMethod      = "method"
	FieldPath        = "path"
	FieldQuery       = "query"
	FieldDuration    = "duration"
	FieldBytes       = "bytes"
	FieldContentType = "contenttype"
	FieldCached      = "cached"
)

var AllFields = []string{
	FieldLayerName,
	FieldLayerParams,
	FieldTenantID,
	FieldIP,
	FieldUserAgent,
	FieldReferer,
	FieldHost,
	FieldMethod,
	FieldPath,
	FieldQuery,
	FieldDuration,
	FieldBytes,
	FieldContentType,
	FieldCached,
}

// `env.` and `secret.` are absent since they're resolved before a module sees its configuration
const (
	extraPrefixContext = "ctx."
	extraPrefixHeader  = "hdr."
)

// Request-scoped values unavailable from the context alone. Populated by the tile handler on success
type FieldSource struct {
	// Exactly as it appeared in the URL
	LayerName   string
	Bytes       int
	ContentType string
}

// Built once at startup so per-request work is limited to reading values
type fieldResolver struct {
	fields      []string
	extraFields map[string]string
}

// Decoded separately so a resolver can be built without knowing the module's own config type
type commonOnly struct {
	Fields      []string
	ExtraFields map[string]string
}

func newFieldResolver(rawConfig map[string]interface{}, errorMessages config.ErrorMessages) (*fieldResolver, error) {
	var cfg commonOnly

	if err := mapstructure.Decode(rawConfig, &cfg); err != nil {
		return nil, err
	}

	for _, f := range cfg.Fields {
		normalized := strings.ToLower(f)

		if !isKnownField(normalized) {
			return nil, fmt.Errorf(errorMessages.EnumError, "analytics.fields", f, AllFields)
		}
	}

	normalized := make([]string, 0, len(cfg.Fields))
	for _, f := range cfg.Fields {
		normalized = append(normalized, strings.ToLower(f))
	}

	return &fieldResolver{fields: normalized, extraFields: cfg.ExtraFields}, nil
}

func isKnownField(name string) bool {
	for _, f := range AllFields {
		if f == name {
			return true
		}
	}

	return false
}

// Nil when nothing is configured, distinguishing "no extra fields" from an empty set
func (r *fieldResolver) Resolve(ctx context.Context, src FieldSource) map[string]any {
	if r == nil || (len(r.fields) == 0 && len(r.extraFields) == 0) {
		return nil
	}

	out := make(map[string]any, len(r.fields)+len(r.extraFields))

	for _, f := range r.fields {
		if v, ok := resolveNamedField(ctx, f, src); ok {
			out[f] = v
		}
	}

	for key, spec := range r.extraFields {
		if v, ok := resolveExtraField(ctx, spec); ok {
			out[key] = v
		}
	}

	return out
}

//nolint:gocritic // A switch on strings is the clearest form here
func resolveNamedField(ctx context.Context, name string, src FieldSource) (any, bool) {
	switch name {
	case FieldLayerName:
		return src.LayerName, true
	case FieldBytes:
		return src.Bytes, true
	case FieldContentType:
		return src.ContentType, true
	case FieldLayerParams:
		matches, ok := pkg.LayerPatternMatchesFromContext(ctx)
		if !ok || matches == nil {
			return nil, false
		}
		return *matches, true
	case FieldTenantID:
		tenant, ok := pkg.TenantIDFromContext(ctx)
		if !ok || tenant == nil {
			return nil, false
		}
		return *tenant, true
	case FieldDuration:
		start, ok := pkg.StartTimeFromContext(ctx)
		if !ok {
			return nil, false
		}
		return time.Since(start).Milliseconds(), true
	case FieldCached:
		cached, ok := pkg.CachedFromContext(ctx)
		if !ok || cached == nil {
			return nil, false
		}
		return *cached, true
	case FieldUserAgent:
		return contextString(ctx, "User-Agent")
	case FieldReferer:
		return contextString(ctx, "Referer")
	case FieldQuery:
		return contextString(ctx, "query-string")
	case FieldIP, FieldHost, FieldMethod, FieldPath:
		return contextString(ctx, name)
	}

	return nil, false
}

// Values without a recognized prefix are literal constants
func resolveExtraField(ctx context.Context, spec string) (any, bool) {
	if after, found := strings.CutPrefix(spec, extraPrefixContext); found {
		return contextValue(ctx, after)
	}

	if after, found := strings.CutPrefix(spec, extraPrefixHeader); found {
		// Headers are stored under their canonical form regardless of client casing
		return contextValue(ctx, http.CanonicalHeaderKey(after))
	}

	return spec, true
}

func contextValue(ctx context.Context, key string) (any, bool) {
	//nolint:staticcheck // The request context uses plain string keys
	v := ctx.Value(key)
	if v == nil {
		return nil, false
	}

	return v, true
}

func contextString(ctx context.Context, key string) (any, bool) {
	v, ok := contextValue(ctx, key)
	if !ok {
		return nil, false
	}

	if s, ok := v.(string); ok {
		return s, true
	}

	return fmt.Sprintf("%v", v), true
}
