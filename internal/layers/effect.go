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
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"slices"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/blur"
	"github.com/anthonynsimon/bild/effect"
	"github.com/anthonynsimon/bild/segment"
)

var intensityModes = []string{"blur", "gaussian", "brightness", "contrast", "gamma", "hue", "saturation", "dilate", "edge detection", "erode", "median", "threshold"}
var noIntensityModes = []string{"emboss", "grayscale", "invert", "sepia", "sharpen", "sobel"}
var allEffectModes = slices.Concat(intensityModes, noIntensityModes)

type EffectConfig struct {
	Mode      string
	Intensity float64
	Provider  map[string]interface{}
}

type Effect struct {
	EffectConfig
	provider Provider
}

func ConstructEffect(config EffectConfig, clientConfig config.ClientConfig, errorMessages config.ErrorMessages, provider Provider) (*Effect, error) {
	if !slices.Contains(allEffectModes, config.Mode) {
		return nil, fmt.Errorf(errorMessages.EnumError, "provider.effect.mode", config.Mode, allEffectModes)
	}

	if slices.Contains(noIntensityModes, config.Mode) && config.Intensity != 0 {
		return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "provider.effect.intensity", "provider.effect.mode="+config.Mode)
	}

	return &Effect{config, provider}, nil
}

func (t Effect) PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	return t.provider.PreAuth(ctx, providerContext)
}

func (t Effect) GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	img, err := t.provider.GenerateTile(ctx, providerContext, tileRequest)

	if err != nil {
		return img, err
	}

	realImage, _, err := image.Decode(bytes.NewReader(*img))

	if err != nil {
		return nil, err
	}

	switch t.Mode {
	case "blur":
		realImage = blur.Box(realImage, t.Intensity)
	case "gaussian":
		realImage = blur.Gaussian(realImage, t.Intensity)
	case "brightness":
		realImage = adjust.Brightness(realImage, t.Intensity)
	case "contrast":
		realImage = adjust.Contrast(realImage, t.Intensity)
	case "gamma":
		realImage = adjust.Gamma(realImage, t.Intensity)
	case "hue":
		realImage = adjust.Hue(realImage, int(t.Intensity))
	case "saturation":
		realImage = adjust.Saturation(realImage, t.Intensity)
	case "dilate":
		realImage = effect.Dilate(realImage, t.Intensity)
	case "edge detection":
		realImage = effect.EdgeDetection(realImage, t.Intensity)
	case "erode":
		realImage = effect.Erode(realImage, t.Intensity)
	case "median":
		realImage = effect.Median(realImage, t.Intensity)
	case "threshold":
		realImage = segment.Threshold(realImage, uint8(t.Intensity))
	case "emboss":
		realImage = effect.Emboss(realImage)
	case "grayscale":
		realImage = effect.Grayscale(realImage)
	case "invert":
		realImage = effect.Invert(realImage)
	case "sepia":
		realImage = effect.Sepia(realImage)
	case "sharpen":
		realImage = effect.Sharpen(realImage)
	case "sobel":
		realImage = effect.Sobel(realImage)
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	err = png.Encode(writer, realImage)
	writer.Flush()
	output := buf.Bytes()

	return &output, err
}
