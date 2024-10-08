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
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"github.com/Michad/tilegroxy/pkg/static"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var packageName = static.GetPackage()

type layerSegment struct {
	value       string
	placeholder bool
}

// Utility method that prepends with checking for dupe segments and propagating errors along
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

// Breaks a pattern string into a series of segments, each of which is either a placeholder or a literal string value
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

func constructValidation(raw map[string]string) (map[string]*regexp.Regexp, error) {
	if raw == nil {
		return nil, nil
	}

	res := make(map[string]*regexp.Regexp)
	errs := make([]error, 0)

	for k, v := range raw {
		var err error
		if v[0] != '^' {
			v = "^" + v
		}
		if v[len(v)-1] != '$' {
			v += "$"
		}

		res[k], err = regexp.Compile(v)

		if err != nil {
			errs = append(errs, err)
		}
	}

	return res, errors.Join(errs...)
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

type Layer struct {
	ID                 string
	Pattern            []layerSegment
	ParamValidator     map[string]*regexp.Regexp
	Config             config.LayerConfig
	Provider           Provider
	Cache              cache.Cache
	ErrorMessages      config.ErrorMessages
	providerContext    ProviderContext
	authMutex          sync.Mutex
	tileAllCounter     metric.Int64Counter
	tileAuthCounter    metric.Int64Counter
	tileErrorCounter   metric.Int64Counter
	tileSuccessCounter metric.Int64Counter
}

func ConstructLayer(rawConfig config.LayerConfig, defaultClientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *LayerGroup, secreter secret.Secreter, datastores *datastore.DatastoreRegistry) (*Layer, error) {
	var err error
	if rawConfig.Client == nil {
		rawConfig.Client = &defaultClientConfig
	} else {
		rawConfig.Client.MergeDefaultsFrom(defaultClientConfig)

	}

	rawConfig.Provider = pkg.ReplaceEnv(rawConfig.Provider)
	if secreter != nil {
		rawConfig.Provider, err = pkg.ReplaceConfigValues(rawConfig.Provider, "secret", secreter.Lookup)
		if err != nil {
			return nil, err
		}
	}

	provider, err := ConstructProvider(rawConfig.Provider, *rawConfig.Client, errorMessages, layerGroup, datastores)

	if err != nil {
		return nil, err
	}

	var segments []layerSegment
	if rawConfig.Pattern != "" && rawConfig.Pattern != rawConfig.ID {
		segments, err = parsePattern(rawConfig.Pattern)
		if err != nil {
			return nil, fmt.Errorf(errorMessages.InvalidParam, "layer.pattern", rawConfig.Pattern)
		}
	} else {
		segments = []layerSegment{{value: rawConfig.ID, placeholder: false}}
	}

	var validator map[string]*regexp.Regexp

	if rawConfig.Pattern != "" && rawConfig.ParamValidator != nil {
		validator, err = constructValidation(rawConfig.ParamValidator)
		if err != nil {
			return nil, err
		}
	}

	meter := otel.Meter(packageName)

	tileAllCounter, err1 := meter.Int64Counter("tilegroxy.tiles.layer."+rawConfig.ID+".request", metric.WithDescription("Number of tile requests for "+rawConfig.ID))
	tileAuthCounter, err2 := meter.Int64Counter("tilegroxy.tiles.layer."+rawConfig.ID+".auth", metric.WithDescription("Number of outgoing authentication checks for "+rawConfig.ID))
	tileErrorCounter, err3 := meter.Int64Counter("tilegroxy.tiles.layer."+rawConfig.ID+".error", metric.WithDescription("Number of tile requests that error during generation for "+rawConfig.ID))
	tileSuccessCounter, err4 := meter.Int64Counter("tilegroxy.tiles.layer."+rawConfig.ID+".success", metric.WithDescription("Number of tile requests that result in a tile for "+rawConfig.ID))

	return &Layer{rawConfig.ID, segments, validator, rawConfig, provider, nil, errorMessages, ProviderContext{}, sync.Mutex{}, tileAllCounter, tileAuthCounter, tileErrorCounter, tileSuccessCounter}, errors.Join(err1, err2, err3, err4)
}

func (l *Layer) authWithProvider(ctx context.Context) error {
	var err error

	if !l.providerContext.AuthBypass {
		l.authMutex.Lock()
		if l.providerContext.AuthExpiration.Before(time.Now()) {
			l.tileAuthCounter.Add(ctx, 1)
			l.providerContext, err = l.Provider.PreAuth(ctx, l.providerContext)
		}
		l.authMutex.Unlock()
	}

	return err
}

func (l *Layer) MatchesName(ctx context.Context, layerName string) bool {

	if doesMatch, matches := match(l.Pattern, layerName); doesMatch {
		if validateParamMatches(matches, l.ParamValidator) {
			layerPatternMatches, _ := pkg.LayerPatternMatchesFromContext(ctx)
			if layerPatternMatches != nil {
				*layerPatternMatches = matches
			}
			return true
		}
	}

	return false
}

func (l *Layer) RenderTileNoCache(ctx context.Context, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	var img *pkg.Image
	var err error

	l.tileAllCounter.Add(ctx, 1)

	err = l.authWithProvider(ctx)

	if err != nil {
		return nil, err
	}

	img, err = l.Provider.GenerateTile(ctx, l.providerContext, tileRequest)

	var authError *pkg.ProviderAuthError
	if errors.As(err, &authError) {
		err = l.authWithProvider(ctx)

		if err != nil {
			return nil, err
		}

		img, err = l.Provider.GenerateTile(ctx, l.providerContext, tileRequest)

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
