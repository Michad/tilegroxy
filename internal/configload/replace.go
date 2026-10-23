// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configload

import (
	"strings"

	"github.com/Michad/tilegroxy/internal/placeholder"
)

// name and id are read before the entity is decoded, so they're resolved on their own
func ResolveEntityKeys(rawConfig map[string]interface{}, resolver *placeholder.Resolver) (map[string]interface{}, error) {
	if rawConfig == nil {
		return nil, nil
	}

	out := make(map[string]interface{}, len(rawConfig))

	for k, v := range rawConfig {
		s, ok := v.(string)
		if !ok || (!strings.EqualFold(k, "name") && !strings.EqualFold(k, "id")) {
			out[k] = v
			continue
		}

		resolved, err := resolver.ResolveString(s)
		if err != nil {
			return nil, err
		}

		out[k] = resolved
	}

	return out, nil
}
