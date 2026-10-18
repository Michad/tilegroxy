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

// Package analytics contains the modules that record tile usage events, selected by the "name" parameter
package analytics

import (
	"fmt"
	"regexp"

	"github.com/Michad/tilegroxy/pkg/config"
)

// Default names for columns holding the always-present event members. Each can be overridden to match an existing table
const (
	ColumnTime      = "time"
	ColumnLayer     = "layer"
	ColumnZ         = "z"
	ColumnX         = "x"
	ColumnY         = "y"
	ColumnUser      = "user_id"
	ColumnExtra     = "extra"
	ColumnLayerName = "layer_name"
)

// Trusted config is still interpolated into SQL, so restricting to plain identifiers keeps typos from becoming flush-time syntax errors
var identifierRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*(\.[A-Za-z_][A-Za-z0-9_$]*)?$`)

func validateIdentifier(name, param string, errorMessages config.ErrorMessages) error {
	if !identifierRegex.MatchString(name) {
		return fmt.Errorf(errorMessages.InvalidParam, param, name)
	}

	return nil
}

// Validates each override as it's merged
func resolveColumns(defaults map[string]string, overrides map[string]string, param string, errorMessages config.ErrorMessages) (map[string]string, error) {
	out := make(map[string]string, len(defaults))

	for k, v := range defaults {
		out[k] = v
	}

	for k, v := range overrides {
		if _, known := out[k]; !known {
			return nil, fmt.Errorf(errorMessages.InvalidParam, param+".columns", k)
		}

		out[k] = v
	}

	for k, v := range out {
		if err := validateIdentifier(v, param+".columns."+k, errorMessages); err != nil {
			return nil, err
		}
	}

	return out, nil
}
