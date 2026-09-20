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

package secret

import (
	"fmt"
	"math"
	"strings"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/go-viper/mapstructure/v2"
)

const defaultWatchInterval = 300

// Generic keys every backend shares, stripped before the backend decodes its own config
var watchConfigKeys = []string{"watch", "watchinterval", "ttl"}

type secretWatchConfig struct {
	Watch         bool
	WatchInterval int
	TTL           int
}

// Pull the generic watch keys out of a secret config, returning the parsed values and the remaining map for the backend to decode
func parseWatchConfig(rawConfig map[string]interface{}, batchSize int, errorMessages config.ErrorMessages) (secretWatchConfig, map[string]interface{}, error) {
	generic := make(map[string]interface{})
	stripped := make(map[string]interface{}, len(rawConfig))

	for k, v := range rawConfig {
		if isWatchConfigKey(k) {
			generic[strings.ToLower(k)] = v
			continue
		}
		stripped[k] = v
	}

	var cfg secretWatchConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		ErrorUnused:      true,
		WeaklyTypedInput: true,
		Result:           &cfg,
	})
	if err != nil {
		return cfg, nil, err
	}

	if err := decoder.Decode(generic); err != nil {
		return cfg, nil, err
	}

	if err := validateWatchConfig(&cfg, generic, batchSize, errorMessages); err != nil {
		return cfg, nil, err
	}

	return cfg, stripped, nil
}

func isWatchConfigKey(key string) bool {
	for _, k := range watchConfigKeys {
		if strings.EqualFold(key, k) {
			return true
		}
	}

	return false
}

func validateWatchConfig(cfg *secretWatchConfig, generic map[string]interface{}, batchSize int, errorMessages config.ErrorMessages) error {
	_, hasInterval := generic["watchinterval"]
	_, hasTTL := generic["ttl"]

	if !cfg.Watch {
		if hasInterval {
			return fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "secret.watch", "secret.watchinterval")
		}
		if hasTTL {
			return fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "secret.watch", "secret.ttl")
		}

		return nil
	}

	if batchSize < 1 {
		return fmt.Errorf(errorMessages.InvalidParam, "secret.watch", "this secret backend does not support change detection")
	}

	if !hasInterval {
		cfg.WatchInterval = defaultWatchInterval
	}

	if cfg.WatchInterval < 1 {
		return fmt.Errorf(errorMessages.RangeError, "secret.watchinterval", 1, math.MaxInt32)
	}

	if cfg.TTL < 0 {
		return fmt.Errorf(errorMessages.RangeError, "secret.ttl", 0, math.MaxInt32)
	}

	return nil
}
