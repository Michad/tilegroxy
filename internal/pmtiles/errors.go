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

package pmtiles

import (
	"fmt"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

// Sparse archives omit empty tiles, so this is routine and reported as a bounds error
type MissingTileError struct {
	Z, X, Y int
}

func (e MissingTileError) coordinates() string {
	return fmt.Sprintf("%d/%d/%d", e.Z, e.X, e.Y)
}

func (e MissingTileError) Error() string {
	return "tile " + e.coordinates() + " is not in the PMTiles archive"
}

func (e MissingTileError) Type() pkg.TypeOfError {
	return pkg.TypeOfErrorBounds
}

func (e MissingTileError) External(messages config.ErrorMessages) string {
	return fmt.Sprintf(messages.TileNotFound, e.coordinates())
}
