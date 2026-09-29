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

package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

type CompositeMVTConfig struct {
	Providers []map[string]interface{}
}

// CompositeVector concatenates the vector tiles of its children, which both MVT and MLT allow.
type CompositeVector struct {
	providers     []layer.Provider
	errorMessages config.ErrorMessages
	contentType   string
}

func init() {
	layer.RegisterProvider(CompositeMVTRegistration{})
}

type CompositeMVTRegistration struct {
}

func (s CompositeMVTRegistration) InitializeConfig() any {
	return CompositeMVTConfig{}
}

func (s CompositeMVTRegistration) Name() string {
	return "compositemvt"
}

func (s CompositeMVTRegistration) DataType(_ any) config.DataType {
	return config.DataTypeMVT
}

func (s CompositeMVTRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(CompositeMVTConfig)

	return newCompositeVector(cfg.Providers, deps, "provider.compositemvt.providers", config.DataTypeMLT, mvtContentType)
}

// Children that produce the other vector format are refused, since concatenating the two corrupts the tile.
func newCompositeVector(childConfigs []map[string]interface{}, deps layer.ProviderDeps, path string, rejected config.DataType, contentType string) (*CompositeVector, error) {
	providers := make([]layer.Provider, 0, len(childConfigs))
	errorSlice := make([]error, 0, len(childConfigs))

	for i, p := range childConfigs {
		provider, err := layer.ConstructProvider(p, deps)
		if err == nil {
			err = checkForInvalidDataType(provider, rejected, path+"."+strconv.Itoa(i), deps.ErrorMessages)
		}

		providers = append(providers, provider)
		errorSlice = append(errorSlice, err)
	}

	errorsFlat := errors.Join(errorSlice...)
	if errorsFlat != nil {
		for _, p := range providers {
			if p != nil {
				errorsFlat = errors.Join(errorsFlat, layer.CloseProvider(context.Background(), p))
			}
		}

		return nil, errorsFlat
	}

	return &CompositeVector{providers: providers, errorMessages: deps.ErrorMessages, contentType: contentType}, nil
}

func (t CompositeVector) Children() []layer.Provider {
	return t.providers
}

func (t CompositeVector) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return providerContext, nil
}

func (t CompositeVector) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	slog.DebugContext(ctx, fmt.Sprintf("Compositing %v providers", len(t.providers)))

	wg := sync.WaitGroup{}
	results := make(chan compositeResult, len(t.providers))

	for i, p := range t.providers {
		wg.Add(1)
		go callCompositingProvider(ctx, providerContext, tileRequest, p, i, results, &wg)
	}

	wg.Wait()

	imgSlice := make([]*pkg.Image, len(t.providers))
	errSlice := make([]error, len(t.providers))
	for range t.providers {
		r := <-results
		errSlice[r.i] = r.err
		imgSlice[r.i] = r.img
	}

	joinError := errors.Join(errSlice...)

	if joinError != nil {
		return nil, joinError
	}

	resultImg := pkg.Image{ContentType: t.contentType, ForceSkipCache: false, Content: []byte{}}
	for _, img := range imgSlice {
		resultImg.Content = slices.Concat(resultImg.Content, img.Content)
		if img.ForceSkipCache {
			resultImg.ForceSkipCache = true
		}
	}

	return &resultImg, nil
}

type compositeResult struct {
	i   int
	img *pkg.Image
	err error
}

func callCompositingProvider(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest, provider layer.Provider, i int, results chan compositeResult, wg *sync.WaitGroup) {
	sent := false
	send := func(img *pkg.Image, err error) {
		if !sent {
			sent = true
			results <- compositeResult{i, img, err}
		}
	}

	defer func() {
		if r := recover(); r != nil {
			send(nil, fmt.Errorf("unexpected composite error %v", r))
		}
		wg.Done()
	}()

	key := strconv.Itoa(i)

	var img *pkg.Image
	var err error
	ac, ok := providerContext.Other[key].(layer.ProviderContext)

	if ok {
		img, err = provider.GenerateTile(ctx, ac, tileRequest)
	} else {
		img, err = provider.GenerateTile(ctx, layer.ProviderContext{}, tileRequest)
	}

	if img == nil && err == nil {
		// img and err are both nil -- that's not right
		err = errors.New("no image returned to compositor")
	}

	send(img, err)
}
