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
	"math"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal/images"
	"github.com/Michad/tilegroxy/internal/placeholder"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

const mimePng = "image/png"

const mvtContentType = "application/vnd.mapbox-vector-tile"
const mltContentType = images.MltContentType

func tileVars(tileRequest pkg.TileRequest, invertY bool, srid uint) (placeholder.TileVars, error) {
	b, err := tileRequest.GetBoundsProjection(srid)
	if err != nil {
		return placeholder.TileVars{}, err
	}

	y := tileRequest.Y
	if invertY {
		y = int(math.Exp2(float64(tileRequest.Z))) - y - 1
	}

	return placeholder.TileVars{Z: tileRequest.Z, X: tileRequest.X, Y: y, Bounds: *b, LayerName: tileRequest.LayerName}, nil
}

func replaceURLPlaceholders(ctx context.Context, tileRequest pkg.TileRequest, tmpl placeholder.Template, invertY bool, srid uint) (string, error) {
	vars, err := tileVars(tileRequest, invertY, srid)
	if err != nil {
		return "", err
	}

	var sink placeholder.URLSink
	tmpl.Render(ctx, vars, &sink)

	return sink.String(), nil
}

// Lives in pkg so library consumers writing Go providers can call it too
func getTile(ctx context.Context, clientConfig config.ClientConfig, url string, authHeaders map[string]string) (*pkg.Image, error) {
	return pkg.GetTile(ctx, clientConfig, url, authHeaders)
}

// Format `<zoom>|<zoom>-<zoom>[,<range>]`, e.g. `4`, `1-5` or `1-3,6`
func ParseZoomString(str string) ([]int, error) {
	const errorMessage = "could not parse zoom %v"

	commaSplit := strings.Split(str, ",")

	var result []int

	for _, entry := range commaSplit {
		dashSplit := strings.Split(entry, "-")

		switch len(dashSplit) {
		case 1:
			singleZoom, err := strconv.Atoi(dashSplit[0])

			if singleZoom < 0 || singleZoom > pkg.MaxZoom {
				return nil, errors.New("zoom out of range")
			}

			if err == nil {
				result = append(result, singleZoom)
			} else {
				return nil, fmt.Errorf(errorMessage, entry)
			}
		case 2:
			start, err := strconv.Atoi(dashSplit[0])
			end, err2 := strconv.Atoi(dashSplit[1])
			if err != nil || err2 != nil {
				return nil, errors.Join(err, err2)
			}

			if end < start {
				return nil, errors.New("zoom range must start before it ends")
			}

			if start < 0 || end > pkg.MaxZoom {
				return nil, errors.New("zoom out of range")
			}

			for i := start; i <= end; i++ {
				result = append(result, i)
			}
		default:
			return nil, fmt.Errorf(errorMessage, entry)
		}
	}

	return result, nil
}

// Rejects a child known to produce a data type the caller can't process
func checkForInvalidDataType(providerToCheck layer.Provider, invalidType config.DataType, path string, errorMessages config.ErrorMessages) error {
	if layer.DescribeTree(providerToCheck).DataType == invalidType {
		return fmt.Errorf(errorMessages.InvalidParam, path, string(invalidType))
	}

	return nil
}
