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

package entities

import (
	"net/http"

	"github.com/Michad/tilegroxy/pkg"
)

type Authentication interface {
	CheckAuthentication(req *http.Request, ctx *pkg.RequestContext) bool
}

type Cache interface {
	Lookup(t pkg.TileRequest) (*pkg.Image, error)
	Save(t pkg.TileRequest, img *pkg.Image) error
}


type Secreter interface {
	Lookup(ctx *pkg.RequestContext, key string) (string, error)
}
