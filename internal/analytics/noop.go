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

package analytics

import (
	"context"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
)

type NoopConfig struct {
	analytics.CommonConfig `mapstructure:",squash"`
}

type Noop struct {
	NoopConfig
}

func init() {
	analytics.RegisterAnalytics(NoopRegistration{})
}

type NoopRegistration struct {
}

func (s NoopRegistration) InitializeConfig() any {
	return NoopConfig{}
}

func (s NoopRegistration) Name() string {
	return "none"
}

func (s NoopRegistration) Initialize(cfgAny any, _ *datastore.DatastoreRegistry, _ config.ErrorMessages) (analytics.Analytics, error) {
	cfg := cfgAny.(NoopConfig)
	return Noop{cfg}, nil
}

func (c Noop) Record(_ context.Context, _ analytics.Event) error {
	return nil
}
