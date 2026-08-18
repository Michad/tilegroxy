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
	"github.com/Michad/tilegroxy/pkg/entities/lifecycle"
)

type CompositeMVTConfig struct {
	Providers []map[string]interface{}
}

type CompositeMVT struct {
	// CompositeMVTConfig
	providers     []layer.Provider
	errorMessages config.ErrorMessages
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

func (s CompositeMVTRegistration) Initialize(cfgAny any, deps layer.ProviderDeps) (layer.Provider, error) {
	cfg := cfgAny.(CompositeMVTConfig)

	providers := make([]layer.Provider, 0, len(cfg.Providers))
	errorSlice := make([]error, 0, len(cfg.Providers))

	for _, p := range cfg.Providers {
		provider, err := layer.ConstructProvider(p, deps)
		providers = append(providers, provider)
		errorSlice = append(errorSlice, err)
	}

	errorsFlat := errors.Join(errorSlice...)
	if errorsFlat != nil {
		return nil, errorsFlat
	}

	return &CompositeMVT{providers: providers, errorMessages: deps.ErrorMessages}, nil
}

// Close releases the child providers. CompositeMVT holds them directly rather than through a
// Layer, so they're unreachable from LayerGroup.Close without this.
func (t CompositeMVT) Close(ctx context.Context) error {
	errs := make([]error, 0, len(t.providers))

	for _, p := range t.providers {
		errs = append(errs, lifecycle.CloseIfCloser(ctx, p))
	}

	return errors.Join(errs...)
}

func (t CompositeMVT) PreAuth(_ context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return providerContext, nil
}

func (t CompositeMVT) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	slog.DebugContext(ctx, fmt.Sprintf("Compositing %v providers", len(t.providers)))

	wg := sync.WaitGroup{}
	errs := make(chan error, len(t.providers))
	imgs := make(chan *pkg.Image, len(t.providers))

	for i, p := range t.providers {
		wg.Add(1)
		go callCompositingProvider(ctx, providerContext, tileRequest, p, i, imgs, errs, &wg)
	}

	wg.Wait()

	imgSlice := make([]*pkg.Image, len(t.providers))
	errSlice := make([]error, len(t.providers))
	for i := range t.providers {
		errSlice[i] = <-errs
		imgSlice[i] = <-imgs
	}

	joinError := errors.Join(errSlice...)

	if joinError != nil {
		return nil, joinError
	}

	resultImg := pkg.Image{ContentType: mvtContentType, ForceSkipCache: false, Content: []byte{}}
	for _, img := range imgSlice {
		resultImg.Content = slices.Concat(resultImg.Content, img.Content)
		if img.ForceSkipCache {
			resultImg.ForceSkipCache = true
		}
	}

	return &resultImg, nil
}

func callCompositingProvider(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest, provider layer.Provider, i int, imgs chan *pkg.Image, errs chan error, wg *sync.WaitGroup) {
	defer func() {
		if r := recover(); r != nil {
			errs <- fmt.Errorf("unexpected composite error %v", r)
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

	if img != nil {
		imgs <- img
	} else if err == nil {
		// img and err are both nil -- that's not right
		err = errors.New("no image returned to compositor")
	}

	errs <- err
}
