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

//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

func Test_Request_ExpectStatusAndHeader(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	inst.Get("/tiles/color/8/12/32").
		ExpectStatus(http.StatusOK).
		ExpectHeader("Content-Type", "image/png")
}

func Test_Request_BadCoordinatesAreRejected(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	inst.Get("/tiles/color/8/ghj/32").ExpectStatus(http.StatusBadRequest)
	inst.Get("/tiles/color/hgkgh/12/32").ExpectStatus(http.StatusBadRequest)
	// A nonexistent layer returns 404 rather than 401 - see issue #766.
	inst.Get("/tiles/nosuchlayer/8/12/32").ExpectStatus(http.StatusNotFound)
}
