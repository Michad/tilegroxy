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
	"sync"
	"time"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/providers"
)

type Layer struct {
	Id              string
	Config          config.LayerConfig
	Provider        providers.Provider
	Cache           *caches.Cache
	ErrorMessages   *config.ErrorMessages
	providerContext providers.ProviderContext
	authMutex       sync.Mutex
}

func ConstructLayer(rawConfig config.LayerConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages) (*Layer, error) {
	if rawConfig.OverrideClient == nil {
		rawConfig.OverrideClient = clientConfig
	}
	provider, error := providers.ConstructProvider(rawConfig.Provider, rawConfig.OverrideClient, errorMessages)

	if error != nil {
		return nil, error
	}

	return &Layer{rawConfig.Id, rawConfig, provider, nil, errorMessages, providers.ProviderContext{}, sync.Mutex{}}, nil
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
			return nil, providers.AuthError{} //TODO: should be a different auth error
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
			return nil, providers.AuthError{} //TODO: should be a different auth error
		}
	}

	var img *internal.Image
	var err error

	err = l.authWithProvider(ctx)

	if err != nil {
		return nil, err
	}

	img, err = l.Provider.GenerateTile(ctx, l.providerContext, tileRequest)

	var authError *providers.AuthError
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
