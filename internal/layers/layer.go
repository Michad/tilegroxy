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
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
)

type layerSegment struct {
	value       string
	placeholder bool
}

// Utility method that prepends with checking for dupe segments and propagating errors along
func app(arr []layerSegment, new layerSegment, errs error) ([]layerSegment, error) {
	if new.placeholder {
		if len(arr) > 0 && arr[0].placeholder {
			errs = errors.Join(errs, errors.New("placeholders without separators"))
		}

		for _, cur := range arr {
			if cur.placeholder && new.value == cur.value {
				errs = errors.Join(errs, errors.New("dupe: "+new.value))
			}
		}
	}

	return slices.Concat([]layerSegment{new}, arr), errs
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
		return app(next, seg, err)
	} else if firstOpening == 0 {
		if firstClosing > 0 {
			seg := layerSegment{value: pattern[1:firstClosing], placeholder: true}
			next, err := parsePattern(pattern[firstClosing+1:])
			return app(next, seg, err)
		} else {
			return []layerSegment{{value: pattern[1:], placeholder: true}}, errors.New("missing }")
		}
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
	}

	return true, matches
}

type Layer struct {
	Id              string
	Pattern         []layerSegment
	Config          config.LayerConfig
	Provider        Provider
	Cache           *caches.Cache
	ErrorMessages   *config.ErrorMessages
	providerContext ProviderContext
	authMutex       sync.Mutex
}

func ConstructLayer(rawConfig config.LayerConfig, defaultClientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, layerGroup *LayerGroup) (*Layer, error) {
	if rawConfig.Client == nil {
		rawConfig.Client = defaultClientConfig
	} else {
		rawConfig.Client.MergeDefaultsFrom(*defaultClientConfig)
	}
	provider, err := ConstructProvider(rawConfig.Provider, rawConfig.Client, errorMessages, layerGroup)

	if err != nil {
		return nil, err
	}

	var segments []layerSegment
	if rawConfig.Pattern != "" && rawConfig.Pattern != rawConfig.Id {
		segments, err = parsePattern(rawConfig.Pattern)
		if err != nil {
			return nil, fmt.Errorf(errorMessages.InvalidParam, "layer.pattern", rawConfig.Pattern)
		}
	} else {
		segments = []layerSegment{{value: rawConfig.Id, placeholder: false}}
	}

	return &Layer{rawConfig.Id, segments, rawConfig, provider, nil, errorMessages, ProviderContext{}, sync.Mutex{}}, nil
}

func (l *Layer) authWithProvider(ctx *internal.RequestContext) error {
	var err error

	if !l.providerContext.AuthBypass {
		l.authMutex.Lock()
		if l.providerContext.AuthExpiration.Before(time.Now()) {
			l.providerContext, err = l.Provider.PreAuth(ctx, l.providerContext)
		}
		l.authMutex.Unlock()
	}

	return err
}

func (l *Layer) RenderTile(ctx *internal.RequestContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	if ctx.LimitLayers {
		if !slices.Contains(ctx.AllowedLayers, l.Id) {
			slog.InfoContext(ctx, "Denying access to non-allowed layer")
			return nil, AuthError{} //TODO: should be a different auth error
		}
	}

	if !ctx.AllowedArea.IsNullIsland() {
		bounds, err := tileRequest.GetBounds()
		if err != nil || !ctx.AllowedArea.Contains(*bounds) {
			slog.InfoContext(ctx, "Denying access to non-allowed area")
			return nil, AuthError{} //TODO: should be a different auth error
		}
	}

	if l.Config.SkipCache {
		return l.RenderTileNoCache(ctx, tileRequest)
	}

	var img *internal.Image
	var err error

	img, err = (*l.Cache).Lookup(tileRequest)

	if img != nil {
		slog.DebugContext(ctx, "Cache hit")
		return img, err
	}

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Cache read error %v\n", err))
	}

	img, err = l.RenderTileNoCache(ctx, tileRequest)

	if err != nil {
		return nil, err
	}

	err = (*l.Cache).Save(tileRequest, img)

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Cache save error %v\n", err))
	}

	return img, nil
}

func (l *Layer) RenderTileNoCache(ctx *internal.RequestContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	if ctx.LimitLayers {
		if !slices.Contains(ctx.AllowedLayers, l.Id) {
			slog.InfoContext(ctx, "Denying access to non-allowed layer")
			return nil, AuthError{} //TODO: should be a different auth error
		}
	}

	var img *internal.Image
	var err error

	err = l.authWithProvider(ctx)

	if err != nil {
		return nil, err
	}

	img, err = l.Provider.GenerateTile(ctx, l.providerContext, tileRequest)

	var authError *AuthError
	if errors.As(err, &authError) {
		err = l.authWithProvider(ctx)

		if err != nil {
			return nil, err
		}

		img, err = l.Provider.GenerateTile(ctx, l.providerContext, tileRequest)

		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return img, nil
}
