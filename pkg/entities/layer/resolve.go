// Copyright 2026 Michael Davis
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

package layer

import (
	"cmp"
	"fmt"
	"log/slog"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

// A center is longitude, latitude, then an optional zoom.
const (
	centerLatIndex  = 1
	centerZoomIndex = 2
)

// An explicit datatype that disagrees with what the provider actually produces is a config error.
func reconcileDataType(configured, reported config.DataType, errorMessages config.ErrorMessages) (config.DataType, error) {
	if !isKnownDataType(configured) {
		return cmp.Or(reported, configured, config.DataTypeUnknown), nil
	}

	if isKnownDataType(reported) && configured != reported {
		return config.DataTypeUnknown, fmt.Errorf(errorMessages.InvalidParam, "layer.datatype", string(configured))
	}

	return configured, nil
}

// Validated operator values can't conflict with each other, so any conflict is resolved by dropping the provider's value.
func resolveMetadata(layerID string, cfg config.LayerMetadata, reported Description, errorMessages config.ErrorMessages) (ResolvedMetadata, error) {
	dataType, err := reconcileDataType(cfg.DataType, reported.DataType, errorMessages)
	if err != nil {
		return ResolvedMetadata{}, err
	}

	limits := Limits{MinZoom: cfg.MinZoom, MaxZoom: cfg.MaxZoom, Bounds: cfg.Bounds}
	w := metadataWarner{layerID}

	adv := Description{DataType: dataType}
	adv.Description = cmp.Or(cfg.Description, reported.Description)
	adv.Attribution = cmp.Or(cfg.Attribution, reported.Attribution)
	adv.Version = cmp.Or(cfg.Version, reported.Version)

	adv.VectorLayers = cfg.VectorLayers
	if adv.VectorLayers == nil {
		adv.VectorLayers = reported.VectorLayers
	}

	adv.MinZoom, adv.MaxZoom = resolveZoom(limits, reported, w)
	adv.Bounds = resolveBounds(limits, reported, w)
	adv.Center = cfg.Center

	if adv.Center != nil {
		adv = keepOperatorCenter(adv, cfg, w)
	} else {
		adv.Center = resolveProviderCenter(adv, reported.Center, w)
	}

	return ResolvedMetadata{Limits: limits, Advertised: adv}, nil
}

func resolveZoom(limits Limits, reported Description, w metadataWarner) (*int, *int) {
	minZoom, maxZoom := limits.MinZoom, limits.MaxZoom

	if minZoom == nil && reported.MinZoom != nil {
		_, hi := zoomRange(nil, limits.MaxZoom)
		if *reported.MinZoom > hi {
			w.warn("minzoom", *reported.MinZoom, "is above the layer's maxzoom")
		} else {
			minZoom = reported.MinZoom
		}
	}

	if maxZoom == nil && reported.MaxZoom != nil {
		lo, _ := zoomRange(minZoom, nil)
		if *reported.MaxZoom < lo {
			w.warn("maxzoom", *reported.MaxZoom, "is below the layer's minzoom")
		} else {
			maxZoom = reported.MaxZoom
		}
	}

	return minZoom, maxZoom
}

func resolveBounds(limits Limits, reported Description, w metadataWarner) config.BoundsConfig {
	if reported.Bounds == (config.BoundsConfig{}) {
		return limits.Bounds
	}

	b := reported.Bounds
	if b.South > b.North || b.West > b.East {
		w.warn("bounds", b, "is not a valid area")
		return limits.Bounds
	}

	if limits.Bounds == (config.BoundsConfig{}) {
		return b
	}

	if !pkg.BoundsFromConfig(b).Intersects(pkg.BoundsFromConfig(limits.Bounds)) {
		w.warn("bounds", b, "lies outside the layer's bounds")
		return limits.Bounds
	}

	return Description{Bounds: b}.Clip(limits.Bounds).Bounds
}

// An operator-set center wins over provider-reported bounds and zoom that would exclude it.
func keepOperatorCenter(adv Description, cfg config.LayerMetadata, w metadataWarner) Description {
	if !centerWithin(adv.Center, adv.Bounds) {
		w.warn("bounds", adv.Bounds, "excludes the layer's center")
		adv.Bounds = cfg.Bounds
	}

	if len(adv.Center) <= centerZoomIndex {
		return adv
	}

	z := adv.Center[centerZoomIndex]
	if adv.MinZoom != nil && z < float64(*adv.MinZoom) && cfg.MinZoom == nil {
		w.warn("minzoom", *adv.MinZoom, "excludes the layer's center")
		adv.MinZoom = nil
	}

	if adv.MaxZoom != nil && z > float64(*adv.MaxZoom) && cfg.MaxZoom == nil {
		w.warn("maxzoom", *adv.MaxZoom, "excludes the layer's center")
		adv.MaxZoom = nil
	}

	return adv
}

func resolveProviderCenter(adv Description, center []float64, w metadataWarner) []float64 {
	if center == nil {
		return nil
	}

	if len(center) <= centerLatIndex || len(center) > centerZoomIndex+1 {
		w.warn("center", center, "is not longitude, latitude and an optional zoom")
		return nil
	}

	if !centerWithin(center, adv.Bounds) {
		w.warn("center", center, "lies outside the layer's bounds")
		return nil
	}

	if len(center) > centerZoomIndex {
		lo, hi := adv.ZoomRange()
		return []float64{center[0], center[centerLatIndex], min(max(center[centerZoomIndex], float64(lo)), float64(hi))}
	}

	return center
}

type metadataWarner struct {
	layerID string
}

func (w metadataWarner) warn(field string, value any, reason string) {
	slog.Warn(fmt.Sprintf("Ignoring %v %v reported by the provider of layer %v: it %v", field, value, w.layerID, reason))
}
