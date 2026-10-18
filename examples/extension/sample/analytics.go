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

package sample

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/Michad/tilegroxy/pkg/entities/analytics"
)

const logFileMode = 0o600

type AnalyticsConfig struct {
	Path string
}

// Appends each event to a file as a line of JSON
type Analytics struct {
	mu   sync.Mutex
	file *os.File
}

func init() {
	analytics.RegisterAnalytics(AnalyticsRegistration{})
}

type AnalyticsRegistration struct{}

func (AnalyticsRegistration) InitializeConfig() any {
	return AnalyticsConfig{}
}

func (AnalyticsRegistration) Name() string {
	return "sample"
}

func (AnalyticsRegistration) Initialize(cfgAny any, deps analytics.AnalyticsDeps) (analytics.Analytics, error) {
	cfg := cfgAny.(AnalyticsConfig)

	if cfg.Path == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, "analytics.path")
	}

	file, err := os.OpenFile(cfg.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, logFileMode)
	if err != nil {
		return nil, err
	}

	return &Analytics{file: file}, nil
}

func (a *Analytics) Record(_ context.Context, event analytics.Event) error {
	line, err := json.Marshal(event)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	_, err = a.file.Write(append(line, '\n'))

	return err
}

// Called on shutdown and hot reload because Analytics implements lifecycle.Closer
func (a *Analytics) Close(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.file.Close()
}
