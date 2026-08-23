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

package pkg

import "github.com/Michad/tilegroxy/pkg/config"

// DataType describes the kind of tile data a Provider produces. Aliased from config.DataType
// (not redefined here) because pkg/config.LayerConfig needs this same type and pkg/config cannot
// import pkg without a cycle (pkg/errors.go already imports pkg/config).
type DataType = config.DataType

const (
	DataTypeRaster  = config.DataTypeRaster
	DataTypeMVT     = config.DataTypeMVT
	DataTypeUnknown = config.DataTypeUnknown
)
