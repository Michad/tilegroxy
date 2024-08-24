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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"

	"github.com/anthonynsimon/bild/blend"
	"github.com/anthonynsimon/bild/transform"
)

const maxProviders = 100

// Allow you to directly reference another layer that uses a pattern with multiple concrete values for the pattern
type BlendLayerConfig struct {
	Pattern string
	Values  []map[string]string
}

type BlendConfig struct {
	Opacity   float64
	Mode      string
	Providers []map[string]interface{}
	Layer     *BlendLayerConfig
}

type Blend struct {
	BlendConfig
	providers []layer.Provider
}

type indexedImg struct {
	int
	image.Image
}

var allBlendModes = []string{"add", "color burn", "color dodge", "darken", "difference", "divide", "exclusion", "lighten", "linear burn", "linear light", "multiply", "normal", "opacity", "overlay", "screen", "soft light", "subtract"}

func init() {
	layer.RegisterProvider(BlendRegistration{})
}

type BlendRegistration struct {
}

func (s BlendRegistration) InitializeConfig() any {
	return BlendConfig{}
}

func (s BlendRegistration) Name() string {
	return "blend"
}

func (s BlendRegistration) Initialize(cfgAny any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *layer.LayerGroup) (layer.Provider, error) {
	cfg := cfgAny.(BlendConfig)
	var err error
	if !slices.Contains(allBlendModes, cfg.Mode) {
		return nil, fmt.Errorf(errorMessages.EnumError, "provider.blend.mode", cfg.Mode, allBlendModes)
	}
	if cfg.Mode != "opacity" && cfg.Opacity != 0 {
		return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "provider.blend.opacity", cfg.Mode)
	}
	var providers []layer.Provider
	if cfg.Layer != nil {
		providers = make([]layer.Provider, len(cfg.Layer.Values))
		for i, lay := range cfg.Layer.Values {
			var ref layer.Provider

			layerName := cfg.Layer.Pattern

			for k, v := range lay {
				layerName = strings.ReplaceAll(layerName, "{"+k+"}", v)
			}

			ref, err = layer.ConstructProvider(map[string]interface{}{"name": "ref", "layer": layerName}, clientConfig, errorMessages, layerGroup)
			if err != nil {
				return nil, err
			}
			providers[i] = ref
		}
	} else {
		providers = make([]layer.Provider, 0, len(cfg.Providers))
		errorSlice := make([]error, 0)

		for _, p := range cfg.Providers {
			provider, err := layer.ConstructProvider(p, clientConfig, errorMessages, layerGroup)
			providers = append(providers, provider) //nolint:makezero //Linter is easily confused if initialized before the make
			errorSlice = append(errorSlice, err)
		}

		errorsFlat := errors.Join(errorSlice...)
		if errorsFlat != nil {
			return nil, errorsFlat
		}
	}

	if len(providers) < 2 || len(providers) > maxProviders {
		return nil, fmt.Errorf(errorMessages.RangeError, "provider.blend.providers.length", 2, maxProviders)
	}

	return &Blend{cfg, providers}, nil
}

func (t Blend) PreAuth(ctx context.Context, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	newProviderContext := layer.ProviderContext{Other: map[string]interface{}{}}

	wg := sync.WaitGroup{}
	errs := make(chan error, len(t.providers))
	acResults := make(chan struct {
		int
		layer.ProviderContext
	}, len(t.providers))

	for i, p := range t.providers {
		var thisPc interface{}

		if providerContext.Other != nil {
			thisPc = providerContext.Other[strconv.Itoa(i)]
		}

		wg.Add(1)
		go func(acObj interface{}, index int, p layer.Provider) {
			defer func() {
				if r := recover(); r != nil {
					errs <- fmt.Errorf("unexpected blend error %v", r)
				}
				wg.Done()
			}()

			var err error
			ac, ok := acObj.(layer.ProviderContext)

			if ok {
				ac, err = p.PreAuth(ctx, ac)
			} else {
				ac, err = p.PreAuth(ctx, layer.ProviderContext{})
			}

			acResults <- struct {
				int
				layer.ProviderContext
			}{index, ac}

			errs <- err
		}(thisPc, i, p)
	}

	wg.Wait()

	errSlice := make([]error, len(t.providers))
	allBypass := true
	nextExp := time.Now().Add(time.Hour)
	for i := range t.providers {
		errSlice[i] = <-errs

		acStruct := <-acResults
		newProviderContext.Other[strconv.Itoa(i)] = acStruct.ProviderContext

		if !acStruct.AuthBypass {
			allBypass = false
		}

		if acStruct.AuthExpiration.Before(nextExp) {
			nextExp = acStruct.AuthExpiration
		}
	}

	newProviderContext.AuthExpiration = nextExp

	if allBypass {
		newProviderContext.AuthBypass = true
	}

	return newProviderContext, errors.Join(errSlice...)
}

func (t Blend) GenerateTile(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	slog.DebugContext(ctx, fmt.Sprintf("Blending together %v providers", len(t.providers)))

	wg := sync.WaitGroup{}
	errs := make(chan error, len(t.providers))
	imgs := make(chan indexedImg, len(t.providers))
	var skipWrite atomic.Bool

	for i, p := range t.providers {
		wg.Add(1)
		go callProvider(ctx, providerContext, tileRequest, p, i, imgs, errs, &wg, &skipWrite)
	}

	wg.Wait()

	errSlice := make([]error, len(t.providers))
	for i := range t.providers {
		errSlice[i] = <-errs
	}

	joinError := errors.Join(errSlice...)

	if joinError != nil {
		return nil, joinError
	}

	imgSlice := make([]image.Image, len(t.providers))
	for range t.providers {
		imgStruct := <-imgs
		imgSlice[imgStruct.int] = imgStruct.Image
	}

	var combinedImg image.Image
	combinedImg = nil

	var size image.Point
	for i, img := range imgSlice {
		curSize := img.Bounds().Max
		slog.Log(ctx, config.LevelTrace, fmt.Sprintf("Image %v size: %v", i, curSize))
		if curSize.X > size.X {
			size.X = curSize.X
		}
		if curSize.Y > size.Y {
			size.Y = curSize.Y
		}
	}
	slog.Log(ctx, config.LevelTrace, fmt.Sprintf("Blended size: %v", size))

	for _, img := range imgSlice {
		combinedImg = t.blendImage(ctx, img, size, combinedImg)
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	err := png.Encode(writer, combinedImg)
	writer.Flush()
	output := buf.Bytes()

	return &pkg.Image{Content: output, ContentType: mimePng, ForceSkipCache: skipWrite.Load()}, err
}

func (t Blend) blendImage(ctx context.Context, img image.Image, size image.Point, combinedImg image.Image) image.Image {
	if img.Bounds().Max != size {
		slog.DebugContext(ctx, fmt.Sprintf("Resizing from %v to %v", img.Bounds().Max, size))
		img = transform.Resize(img, size.X, size.Y, transform.NearestNeighbor)
	}

	if combinedImg == nil {
		combinedImg = img
	} else {
		switch t.Mode {
		case "add":
			combinedImg = blend.Add(img, combinedImg)
		case "color burn":
			combinedImg = blend.ColorBurn(img, combinedImg)
		case "color dodge":
			combinedImg = blend.ColorDodge(img, combinedImg)
		case "darken":
			combinedImg = blend.Darken(img, combinedImg)
		case "difference":
			combinedImg = blend.Difference(img, combinedImg)
		case "divide":
			combinedImg = blend.Divide(img, combinedImg)
		case "exclusion":
			combinedImg = blend.Exclusion(img, combinedImg)
		case "lighten":
			combinedImg = blend.Lighten(img, combinedImg)
		case "linear burn":
			combinedImg = blend.LinearBurn(img, combinedImg)
		case "linear light":
			combinedImg = blend.LinearLight(img, combinedImg)
		case "multiply":
			combinedImg = blend.Multiply(img, combinedImg)
		case "normal":
			combinedImg = blend.Normal(img, combinedImg)
		case "opacity":
			combinedImg = blend.Opacity(img, combinedImg, t.Opacity)
		case "overlay":
			combinedImg = blend.Overlay(img, combinedImg)
		case "screen":
			combinedImg = blend.Screen(img, combinedImg)
		case "soft light":
			combinedImg = blend.SoftLight(img, combinedImg)
		case "subtract":
			combinedImg = blend.Subtract(img, combinedImg)
		}
	}
	return combinedImg
}

func callProvider(ctx context.Context, providerContext layer.ProviderContext, tileRequest pkg.TileRequest, provider layer.Provider, i int, imgs chan indexedImg, errs chan error, wg *sync.WaitGroup, skipWrite *atomic.Bool) {
	defer func() {
		if r := recover(); r != nil {
			errs <- fmt.Errorf("unexpected blend error %v", r)
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
		if img.ForceSkipCache {
			skipWrite.Store(true)
		}

		realImage, _, err2 := image.Decode(bytes.NewReader(img.Content))
		err = errors.Join(err, err2)

		imgs <- struct {
			int
			image.Image
		}{i, realImage}
	} else if err == nil {
		//img and err are both nil -- that's not right
		err = errors.New("no image returned to blender")
	}

	errs <- err
}
