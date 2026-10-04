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

package providers

import (
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/layer"
)

// Auth bounds replace the configured bounds per request, so they aren't a reliable limit
func clipToCrop(d layer.Description, bounds pkg.Bounds, boundsFromAuth bool) layer.Description {
	if boundsFromAuth || bounds.IsNullIsland() {
		return d
	}

	return d.Clip(bounds.ToConfig())
}
