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
	"log/slog"
	"os"
	"reflect"
	"strings"
)

// Find any string values that start with `keyTag.keyName` and replace it with replacer(keyName). Replaces the full value. Used for avoiding secrets in config so your configuration can be placed in source control
func ReplaceConfigValues(rawConfig map[string]interface{}, keyTag string, replacer func(string) (string, error)) (map[string]interface{}, error) {
	result, err := replaceConfigValuesAny(rawConfig, keyTag, replacer)
	if err != nil {
		return nil, err
	}

	return result.(map[string]interface{}), nil
}

// replaceConfigValuesAny recursively walks arbitrary config values looking for strings tagged
// "keyTag.keyName" to replace. It recurses by reflect.Kind rather than by concrete type because
// config values arrive in more shapes than a type switch can enumerate: mapstructure-decoded
// fields like ClientConfig.Headers are map[string]string, not map[string]interface{}.
func replaceConfigValuesAny(v any, keyTag string, replacer func(string) (string, error)) (any, error) {
	if v == nil {
		return nil, nil
	}

	if vStr, ok := v.(string); ok {
		if strings.Index(vStr, keyTag+".") == 0 {
			varName := vStr[len(keyTag)+1:]
			slog.Debug("Replacing " + keyTag + " var " + varName)
			return replacer(varName)
		}
		return vStr, nil
	}

	rv := reflect.ValueOf(v)

	switch rv.Kind() { //nolint:exhaustive // the default arm handles all remaining kinds
	case reflect.Map:
		result := reflect.MakeMap(rv.Type())
		for _, key := range rv.MapKeys() {
			original := rv.MapIndex(key)
			replaced, err := replaceConfigValuesAny(original.Interface(), keyTag, replacer)
			if err != nil {
				return nil, err
			}
			// A nil replacement means the original was nil, as a YAML key written with no value
			// (`ttl:`) parses to. Convert would panic on the zero Value it produces.
			if replaced == nil {
				result.SetMapIndex(key, original)
				continue
			}
			result.SetMapIndex(key, reflect.ValueOf(replaced).Convert(rv.Type().Elem()))
		}
		return result.Interface(), nil
	case reflect.Slice, reflect.Array:
		// Arrays come back as slices. Config parsed from YAML/JSON never contains arrays, so this
		// only affects hand-constructed input.
		result := reflect.MakeSlice(reflect.SliceOf(rv.Type().Elem()), rv.Len(), rv.Len())
		for i := range rv.Len() {
			original := rv.Index(i)
			replaced, err := replaceConfigValuesAny(original.Interface(), keyTag, replacer)
			if err != nil {
				return nil, err
			}
			// See the nil note in the Map branch above.
			if replaced == nil {
				result.Index(i).Set(original)
				continue
			}
			result.Index(i).Set(reflect.ValueOf(replaced).Convert(rv.Type().Elem()))
		}
		return result.Interface(), nil
	default:
		return v, nil
	}
}

// Find any string values that start with `env.` and interpret the rest as an environment variable. Replaces the full value with the contents of the respective environment variable. Useful for avoiding secrets in config so your configuration can be placed in source control
func ReplaceEnv(rawConfig map[string]interface{}) map[string]interface{} {
	result, _ := ReplaceConfigValues(rawConfig, "env", func(s string) (string, error) { return os.Getenv(s), nil })

	return result
}
