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

package server

import (
	"fmt"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"
	"github.com/Michad/tilegroxy/pkg/entities/health"
)

// ValidateConfig runs the server-level checks that serving performs, without binding ports or opening log files
func ValidateConfig(cfg *config.Config) error {
	if cfg.Server.Encrypt != nil && cfg.Server.Encrypt.Domain == "" {
		return fmt.Errorf(cfg.Error.Messages.ParamRequired, "server.encrypt.domain")
	}

	if err := ValidateCORS(cfg.Server.CORS, cfg.Error.Messages); err != nil {
		return err
	}

	return validateAllCacheControl(cfg)
}

// ValidateHealthChecks constructs each configured health check without scheduling it
func ValidateHealthChecks(cfg *config.Config, ent *entities.Entities) error {
	if !cfg.Health.Enabled {
		return nil
	}

	for _, checkCfg := range cfg.Health.Checks {
		if _, err := health.ConstructHealthCheck(checkCfg, ent.LayerGroup, ent.Caches, cfg); err != nil {
			return err
		}
	}

	return nil
}
