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

package authentication

import (
	"context"
	"net/http"

	"github.com/Michad/tilegroxy/pkg"
)

// A struct that wraps all other auths in order to add in instrumentation, specifically child spans for tracing flow. This is used even when telemetry is disabled but OTEL handles no-op'ing in that case so performance impact is minimal
type AuthWrapper struct {
	Name string
	Auth Authentication
}

func (w AuthWrapper) CheckAuthentication(ctx context.Context, req *http.Request) bool {
	newCtx, span := pkg.MakeChildSpan(ctx, nil, "Authentication", w.Name, "CheckAuthentication")
	defer span.End()

	return w.Auth.CheckAuthentication(newCtx, req)
}
