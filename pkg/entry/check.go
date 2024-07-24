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

package tg

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Michad/tilegroxy/pkg/config"
)

type CheckOptions struct {
	Echo bool
}

func CheckConfig(cfg *config.Config, opts CheckOptions, out io.Writer) error {
	var err error
	_, _, err = configToEntities(*cfg)

	if err != nil {
		return err
	}

	if cfg != nil && opts.Echo {
		enc := json.NewEncoder(out)
		enc.SetIndent(" ", "  ")
		enc.Encode(cfg)
	} else {
		fmt.Fprintln(out, "Valid")
	}

	return nil
}
