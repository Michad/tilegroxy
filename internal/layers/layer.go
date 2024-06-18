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
	"sync"
	"time"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/providers"
)

type Layer struct {
	Id            string
	Config        config.LayerConfig
	Provider      providers.Provider
	Cache         *caches.Cache
	ErrorMessages *config.ErrorMessages
	authContext   *providers.AuthContext
	authMutex     sync.Mutex
}

func ConstructLayer(rawConfig config.LayerConfig, errorMessages *config.ErrorMessages) (*Layer, error) {
	provider, error := providers.ConstructProvider(rawConfig.Provider, errorMessages)

	if error != nil {
		return nil, error
	}

	return &Layer{rawConfig.Id, rawConfig, provider, nil, errorMessages, nil, sync.Mutex{}}, nil
}

func (l *Layer) authWithProvider() error {
	var err error

	l.authMutex.Lock()
	if l.authContext == nil || l.authContext.Expiration.Before(time.Now()) {
		err = l.Provider.PreAuth(l.authContext)
	}
	l.authMutex.Unlock()

	return err
}

func (l *Layer) RenderTileNoCache(tileRequest internal.TileRequest) (*internal.Image, error) {
	var img *internal.Image
	var err error

	if l.authContext == nil || l.authContext.Expiration.Before(time.Now()) {
		err = l.authWithProvider()
	}

	if err != nil {
		return nil, err
	}

	img, err = l.Provider.GenerateTile(l.authContext, l.Config.OverrideClient, l.ErrorMessages, tileRequest)

	var authError *providers.AuthError
	if errors.As(err, &authError) {
		err = l.authWithProvider()

		if err != nil {
			return nil, err
		}

		img, err = l.Provider.GenerateTile(l.authContext, l.Config.OverrideClient, l.ErrorMessages, tileRequest)

		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return img, nil
}
