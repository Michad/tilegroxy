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
	"bufio"
	"bytes"
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

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"

	"github.com/anthonynsimon/bild/blend"
	"github.com/anthonynsimon/bild/transform"
)

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
	providers []*Provider
}

var allBlendModes = []string{"add", "color burn", "color dodge", "darken", "difference", "divide", "exclusion", "lighten", "linear burn", "linear light", "multiply", "normal", "opacity", "overlay", "screen", "soft light", "subtract"}

func ConstructBlend(config BlendConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, providers []*Provider, layerGroup *LayerGroup) (*Blend, error) {
	var err error
	if !slices.Contains(allBlendModes, config.Mode) {
		return nil, fmt.Errorf(errorMessages.EnumError, "provider.blend.mode", config.Mode, allBlendModes)
	}
	if config.Mode != "opacity" && config.Opacity != 0 {
		return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "provider.blend.opacity", config.Mode)
	}
	if config.Layer != nil {
		providers = make([]*Provider, len(config.Layer.Values))
		for i, lay := range config.Layer.Values {
			var ref Provider

			layerName := config.Layer.Pattern

			for k, v := range lay {
				layerName = strings.ReplaceAll(layerName, "{"+k+"}", v)
			}

			cfg := RefConfig{Layer: layerName}
			ref, err = ConstructRef(cfg, clientConfig, errorMessages, layerGroup)
			if err != nil {
				return nil, err
			}
			providers[i] = &ref
		}
	}

	if len(providers) < 2 || len(providers) > 100 {
		return nil, fmt.Errorf(errorMessages.RangeError, "provider.blend.providers.length", 2, 100)
	}

	return &Blend{config, providers}, nil
}

func (t Blend) PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	if providerContext.Other == nil {
		providerContext.Other = map[string]interface{}{}
	}

	wg := sync.WaitGroup{}
	errs := make(chan error, len(t.providers))
	acResults := make(chan struct {
		int
		ProviderContext
	}, len(t.providers))

	for i, p := range t.providers {
		wg.Add(1)
		go func(acObj interface{}, index int, p *Provider) {
			var err error
			ac, ok := acObj.(ProviderContext)

			if ok {
				ac, err = (*p).PreAuth(ctx, ac)
			} else {
				ac, err = (*p).PreAuth(ctx, ProviderContext{})
			}

			acResults <- struct {
				int
				ProviderContext
			}{index, ac}

			errs <- err

			wg.Done()
		}(providerContext.Other[strconv.Itoa(i)], i, p)
	}

	wg.Wait()

	errSlice := make([]error, len(t.providers))
	for i := range t.providers {
		errSlice[i] = <-errs

		acStruct := <-acResults
		providerContext.Other[strconv.Itoa(i)] = acStruct.ProviderContext
	}

	return providerContext, errors.Join(errSlice...)
}

func (t Blend) GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	slog.DebugContext(ctx, fmt.Sprintf("Blending together %v providers", len(t.providers)))

	wg := sync.WaitGroup{}
	errs := make(chan error, len(t.providers))
	imgs := make(chan struct {
		int
		image.Image
	}, len(t.providers))

	for i, p := range t.providers {
		wg.Add(1)
		go func(key string, i int, p *Provider) {
			var img *internal.Image
			var err error
			ac, ok := providerContext.Other[key].(ProviderContext)

			if ok {
				img, err = (*p).GenerateTile(ctx, ac, tileRequest)
			} else {
				img, err = (*p).GenerateTile(ctx, ProviderContext{}, tileRequest)
			}

			realImage, _, err2 := image.Decode(bytes.NewReader(*img))

			imgs <- struct {
				int
				image.Image
			}{i, realImage}

			errs <- errors.Join(err, err2)

			wg.Done()
		}(strconv.Itoa(i), i, p)
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
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	err := png.Encode(writer, combinedImg)
	writer.Flush()
	output := buf.Bytes()

	return &output, err
}
