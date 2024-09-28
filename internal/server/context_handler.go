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

package server

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
)

type httpContextHandler struct {
	http.Handler
	errCfg config.ErrorConfig
}

func (h httpContextHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqContext := pkg.NewRequestContext(req)
	defer func() {
		if err := recover(); err != nil {
			writeErrorMessage(reqContext, w, &h.errCfg, pkg.TypeOfErrorOther, fmt.Sprint(err), "Unexpected Internal Server Error", debug.Stack())
		}
	}()

	h.Handler.ServeHTTP(w, req.WithContext(reqContext))
}
