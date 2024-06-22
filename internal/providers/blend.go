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
	"slices"
	"strconv"
	"sync"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"

	"github.com/anthonynsimon/bild/blend"
)

type BlendConfig struct {
	Opacity   float64
	Mode      string
	Providers []map[string]interface{}
}

type Blend struct {
	BlendConfig
	providers []*Provider
}

var allBlendModes = []string{"add", "color burn", "color dodge", "darken", "difference", "divide", "exclusion", "lighten", "linear burn", "linear light", "multiply", "normal", "opacity", "overlay", "screen", "soft light", "subtract"}

func ConstructBlend(config BlendConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, providers []*Provider) (*Blend, error) {
	if !slices.Contains(allBlendModes, config.Mode) {
		return nil, fmt.Errorf(errorMessages.EnumError, "provider.blend.mode", config.Mode, allBlendModes)
	}
	if config.Mode != "opacity" && config.Opacity != 0 {
		return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "provider.blend.opacity", config.Mode)
	}
	if len(providers) < 2 || len(providers) > 100 {
		return nil, fmt.Errorf(errorMessages.RangeError, "provider.blend.providers.length", 2, 100)
	}

	return &Blend{config, providers}, nil
}

func (t Blend) PreAuth(ctx context.Context, authContext AuthContext) (AuthContext, error) {
	if authContext.Other == nil {
		authContext.Other = map[string]interface{}{}
	}

	wg := sync.WaitGroup{}
	errs := make(chan error, len(t.providers))
	acResults := make(chan struct {
		int
		AuthContext
	}, len(t.providers))

	for i, p := range t.providers {
		wg.Add(1)
		go func(acObj interface{}, index int, p *Provider) {
			var err error
			ac, ok := acObj.(AuthContext)

			if ok {
				ac, err = (*p).PreAuth(ctx, ac)
			} else {
				ac, err = (*p).PreAuth(ctx, AuthContext{})
			}

			acResults <- struct {
				int
				AuthContext
			}{index, ac}

			errs <- err

			wg.Done()
		}(authContext.Other[strconv.Itoa(i)], i, p)
	}

	wg.Wait()

	errSlice := make([]error, len(t.providers))
	for i := range t.providers {
		errSlice[i] = <-errs

		acStruct := <-acResults
		authContext.Other[strconv.Itoa(i)] = acStruct.AuthContext
	}

	return authContext, errors.Join(errSlice...)
}

func (t Blend) GenerateTile(ctx context.Context, authContext AuthContext, tileRequest internal.TileRequest) (*internal.Image, error) {
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
			ac, ok := authContext.Other[key].(AuthContext)

			if ok {
				img, err = (*p).GenerateTile(ctx, ac, tileRequest)
			} else {
				img, err = (*p).GenerateTile(ctx, AuthContext{}, tileRequest)
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

	for _, img := range imgSlice {
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
