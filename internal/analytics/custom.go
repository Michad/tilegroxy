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
	"fmt"
	"os"
	"reflect"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/analytics"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

type CustomConfig struct {
	analytics.CommonConfig `mapstructure:",squash"`
	// Contains the go code to record events as a file.
	File string
	// Contains the go code to record events inline.
	Script string
	// Any other configuration parameters are passed through to the script.
	Params map[string]interface{} `mapstructure:",remain"`
}

type Custom struct {
	CustomConfig
	recordFunc func(context.Context, []analytics.Event, map[string]interface{}, config.ErrorMessages) error
	errorMsgs  config.ErrorMessages
	batcher    *analytics.Batcher
}

func init() {
	analytics.RegisterAnalytics(CustomRegistration{})
}

type CustomRegistration struct {
}

func (s CustomRegistration) InitializeConfig() any {
	return CustomConfig{}
}

func (s CustomRegistration) Name() string {
	return "custom"
}

func (s CustomRegistration) Initialize(cfgAny any, deps analytics.AnalyticsDeps) (analytics.Analytics, error) {
	cfg := cfgAny.(CustomConfig)

	if cfg.File == "" && cfg.Script == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.OneOfRequired, "analytics.custom.file, analytics.custom.script")
	}

	if cfg.File != "" && cfg.Script != "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamsMutuallyExclusive, "analytics.custom.file", "analytics.custom.script")
	}

	i := interp.New(interp.Options{Unrestricted: true})

	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, err
	}

	if err := i.Use(interp.Symbols); err != nil {
		return nil, err
	}

	err := i.Use(interp.Exports{
		"tilegroxy/tilegroxy": map[string]reflect.Value{
			"Context":        reflect.ValueOf((*context.Context)(nil)),
			"AnalyticsEvent": reflect.ValueOf((*analytics.Event)(nil)),
			"ErrorMessages":  reflect.ValueOf((*config.ErrorMessages)(nil)),
		}})
	if err != nil {
		return nil, err
	}

	script := cfg.Script

	if cfg.File != "" {
		scriptBytes, err := os.ReadFile(cfg.File)
		if err != nil {
			return nil, err
		}
		script = string(scriptBytes)
	}

	if _, err := i.Eval(script); err != nil {
		return nil, fmt.Errorf(deps.ErrorMessages.ScriptError, "analytics.custom", err)
	}

	recordVal, err := i.Eval("custom.record")
	if err != nil {
		return nil, fmt.Errorf(deps.ErrorMessages.ScriptError, "analytics.custom", err)
	}

	recordFunc, ok := recordVal.Interface().(func(context.Context, []analytics.Event, map[string]interface{}, config.ErrorMessages) error)
	if !ok {
		return nil, fmt.Errorf(deps.ErrorMessages.ScriptError, "analytics.custom", "record function has the wrong signature")
	}

	batchCfg, err := analytics.ApplyBatchDefaults(cfg.Batch, deps.ErrorMessages)
	if err != nil {
		return nil, err
	}

	id := cfg.ID
	if id == "" {
		id = s.Name()
	}

	c := &Custom{CustomConfig: cfg, recordFunc: recordFunc, errorMsgs: deps.ErrorMessages}

	batcher, err := analytics.NewBatcher(id, batchCfg, c.flush)
	if err != nil {
		return nil, err
	}

	c.batcher = batcher

	return c, nil
}

func (c *Custom) Record(ctx context.Context, event analytics.Event) error {
	return c.batcher.Add(ctx, event)
}

// flush invokes the script once per batch, not once per event. Yaegi calls carry meaningful overhead so
// batching keeps that cost off the per-tile path
func (c *Custom) flush(ctx context.Context, events []analytics.Event) error {
	return c.recordFunc(ctx, events, c.Params, c.errorMsgs)
}

func (c *Custom) Close(ctx context.Context) error {
	return c.batcher.Close(ctx)
}
