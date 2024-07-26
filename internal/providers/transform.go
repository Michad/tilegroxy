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
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"math"
	"os"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

type TransformConfig struct {
	Threads  int
	File     string
	Formula  string
	Provider map[string]interface{}
}

type Transform struct {
	TransformConfig
	provider      layer.Provider
	transformFunc func(uint8, uint8, uint8, uint8) (uint8, uint8, uint8, uint8)
}

func init() {
	layer.RegisterProvider(TransformRegistration{})
}

type TransformRegistration struct {
}

func (s TransformRegistration) InitializeConfig() any {
	return TransformConfig{}
}

func (s TransformRegistration) Name() string {
	return "transform"
}

func (s TransformRegistration) Initialize(cfgAny any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, layerGroup *layer.LayerGroup) (layer.Provider, error) {
	cfg := cfgAny.(TransformConfig)
	var err error

	if cfg.Threads == 0 {
		cfg.Threads = 1
	}

	provider, err := layer.ConstructProvider(cfg.Provider, clientConfig, errorMessages, layerGroup)
	if err != nil {
		return nil, err
	}

	i := interp.New(interp.Options{Unrestricted: true})
	err = i.Use(stdlib.Symbols)
	if err != nil {
		return nil, err
	}

	var script string

	if cfg.File != "" {
		scriptBytes, err := os.ReadFile(cfg.File)
		if err != nil {
			return nil, err
		}
		script = string(scriptBytes)
	} else {
		script = cfg.Formula
	}

	_, err = i.Eval(script)
	if err != nil {
		return nil, err
	}

	transformVal, err := i.Eval("transform")
	if err != nil {
		return nil, err
	}

	transformFunc := transformVal.Interface().(func(uint8, uint8, uint8, uint8) (uint8, uint8, uint8, uint8))

	return &Transform{cfg, provider, transformFunc}, nil
}

func (t Transform) PreAuth(ctx *pkg.RequestContext, providerContext layer.ProviderContext) (layer.ProviderContext, error) {
	return t.provider.PreAuth(ctx, providerContext)
}

func (t Transform) transform(ctx *pkg.RequestContext, col color.Color) color.Color {
	r1, g1, b1, a1 := col.RGBA()
	r1b := uint8(r1)
	g1b := uint8(g1)
	b1b := uint8(b1)
	a1b := uint8(a1)

	r2, g2, b2, a2 := t.transformFunc(r1b, g1b, b1b, a1b)

	result := color.RGBA{r2, g2, b2, a2}

	slog.Log(ctx, config.LevelAbsurd, fmt.Sprintf("Converted %v to %v", col, result))

	return result
}

func (t Transform) GenerateTile(ctx *pkg.RequestContext, providerContext layer.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	img, err := t.provider.GenerateTile(ctx, providerContext, tileRequest)

	if err != nil {
		return img, err
	}

	realImage, _, err := image.Decode(bytes.NewReader(*img))

	if err != nil {
		return nil, err
	}

	resultImage := image.NewRGBA(realImage.Bounds())

	min := realImage.Bounds().Min
	max := realImage.Bounds().Max
	size := max.Sub(min)
	pixelCount := size.X * size.Y

	// Split up all the requests for N threads
	numPixelPerThread := int(math.Floor(float64(pixelCount) / float64(t.Threads)))
	pixelSplit := make([][]int, 0, t.Threads)

	for i := range t.Threads {
		chunkStart := i * numPixelPerThread
		var chunkEnd int
		if i == t.Threads-1 {
			chunkEnd = pixelCount
		} else {
			chunkEnd = int(math.Min(float64(chunkStart+numPixelPerThread), float64(pixelCount)))
		}

		pixelSplit = append(pixelSplit, []int{chunkStart, chunkEnd})
	}

	var wg sync.WaitGroup
	wg.Add(t.Threads)

	for tid := range t.Threads {
		pixelRange := pixelSplit[tid]

		go func(iStart int, iEnd int) {
			defer func() {
				if r := recover(); r != nil {
					slog.Error(fmt.Sprintf("unexpected transform error! %v", r))
				}
				wg.Done()
			}()

			for i := iStart; i < iEnd; i++ {
				dX := i % size.X
				dY := i / size.X

				x := dX + min.X
				y := dY + min.Y

				c1 := realImage.At(x, y)
				c2 := t.transform(ctx, c1)

				resultImage.Set(x, y, c2)
			}
		}(pixelRange[0], pixelRange[1])
	}

	wg.Wait()

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	err = png.Encode(writer, resultImage)
	writer.Flush()
	output := buf.Bytes()

	return &output, err
}
