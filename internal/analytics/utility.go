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

// Package analytics contains the analytics modules that record tile usage events to various destinations.
// Modules are selected by the "name" parameter in configuration
package analytics

import (
	"fmt"
	"regexp"

	"github.com/Michad/tilegroxy/pkg/config"
)

// The default names for the columns holding the always-present members of an event. Any of these can be
// overridden to match a table that already exists
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

// Table and column names come from configuration, which the security model treats as trusted, but they're
// still interpolated into SQL instead of bound. Restricting them to plain identifiers keeps a config typo
// from becoming a syntax error at flush time. Event values are always bound parameters, never formatted in
var identifierRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*(\.[A-Za-z_][A-Za-z0-9_$]*)?$`)

// validateIdentifier checks a table or column name before it's used in a statement
func validateIdentifier(name, param string, errorMessages config.ErrorMessages) error {
	if !identifierRegex.MatchString(name) {
		return fmt.Errorf(errorMessages.InvalidParam, param, name)
	}

	return nil
}

// resolveColumns merges the configured column name overrides over the defaults, validating each
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
