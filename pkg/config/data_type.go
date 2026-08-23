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

package config

// DataType describes the kind of tile data a Provider produces. Lives here (not in pkg) because
// pkg/errors.go already imports pkg/config, so pkg/config cannot import pkg back without a cycle.
type DataType string

const (
	DataTypeRaster  DataType = "raster"
	DataTypeMVT     DataType = "mvt"
	DataTypeUnknown DataType = "unknown"
)

// BoundsConfig is the config-layer shape of a geographic bounding box. It intentionally omits
// pkg.Bounds's SRID field (config-level bounds are always WGS-84) and lives here rather than
// reusing pkg.Bounds directly for the same import-cycle reason as DataType above.
type BoundsConfig struct {
	South float64
	North float64
	West  float64
	East  float64
}
