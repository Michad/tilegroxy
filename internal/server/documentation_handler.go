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
	"log/slog"
	"net/http"

	"github.com/Michad/tilegroxy/internal/website"
)

type documentationHandler struct {
	defaultHandler
}

func (h documentationHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	slog.DebugContext(ctx, "server: documentation handler started")
	defer slog.DebugContext(ctx, "server: documentation handler ended")

	if req.Method != http.MethodGet {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := req.PathValue("path")

	data, contentType, err := website.ReadDocumentationFile(path)

	if err != nil {
		writeError(ctx, w, &h.config.Error, err)
		return
	}

	if contentType != "" {
		w.Header().Add("Content-Type", contentType)
	}

	w.WriteHeader(http.StatusOK)
	_, err = w.Write(data)

	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Unable to write to documentation request due to %v", err))
	}
}
