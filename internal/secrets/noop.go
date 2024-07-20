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

package secrets

import (
	"fmt"

	"github.com/Michad/tilegroxy/pkg/config"
)

type NoopConfig struct {
}

type Noop struct {
	NoopConfig
	errorMessages config.ErrorMessages
}

func ConstructNoopConfig(cfg NoopConfig, errorMessages config.ErrorMessages) (*Noop, error) {
	return &Noop{cfg, errorMessages}, nil
}

func (s Noop) Lookup(key string) (string, error) {
	return "", fmt.Errorf(s.errorMessages.ParamRequired, key)
}
