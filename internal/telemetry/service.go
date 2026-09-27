// Copyright 2026 The tilegroxy Authors
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

package telemetry

import (
	"slices"

	"github.com/Michad/tilegroxy/pkg/static"
	"go.opentelemetry.io/otel/attribute"
)

var serviceAttributes = func() []attribute.KeyValue {
	version, ref, buildDate := static.GetVersionInformation()

	return []attribute.KeyValue{
		attribute.String("service.name", "tilegroxy"),
		attribute.String("service.version", version+"-"+ref),
		attribute.String("service.build", buildDate),
	}
}()

// ServiceAttributes returns the service.* span attributes followed by extra.
func ServiceAttributes(extra ...attribute.KeyValue) []attribute.KeyValue {
	return append(slices.Clip(serviceAttributes), extra...)
}
