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

package layers

import (
	"os"
	"reflect"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

type CustomConfig struct {
	File   string
	Script string                 //Contains the go code of the provider inline.
	Params map[string]interface{} `mapstructure:",remain"`
}

type Custom struct {
	CustomConfig
	clientConfig     config.ClientConfig
	errorMessages    config.ErrorMessages
	interp           *interp.Interpreter
	preAuthFunc      func(*internal.RequestContext, ProviderContext, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (ProviderContext, error)
	generateTileFunc func(*internal.RequestContext, ProviderContext, internal.TileRequest, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (*internal.Image, error)
}

func ConstructCustom(cfg CustomConfig, clientConfig config.ClientConfig, errorMessages config.ErrorMessages) (*Custom, error) {
	i := interp.New(interp.Options{Unrestricted: true})
	i.Use(stdlib.Symbols)
	i.Use(interp.Exports{
		"tilegroxy/tilegroxy": map[string]reflect.Value{
			"RequestContext":  reflect.ValueOf((*internal.RequestContext)(nil)),
			"ProviderContext": reflect.ValueOf((*ProviderContext)(nil)),
			"TileRequest":     reflect.ValueOf((*internal.TileRequest)(nil)),
			"ClientConfig":    reflect.ValueOf((*config.ClientConfig)(nil)),
			"ErrorMessages":   reflect.ValueOf((*config.ErrorMessages)(nil)),
			"Image":           reflect.ValueOf((*internal.Image)(nil)),
			"AuthError":       reflect.ValueOf((*AuthError)(nil)),
			"GetTile":         reflect.ValueOf(getTile),
		}})

	var err error
	var script string

	if cfg.File != "" {
		scriptBytes, err := os.ReadFile(cfg.File)
		if err != nil {
			return nil, err
		}
		script = string(scriptBytes)
	} else {
		script = cfg.Script
	}

	_, err = i.Eval(script)
	if err != nil {
		return nil, err
	}

	preAuthVal, err := i.Eval("custom.preAuth")
	if err != nil {
		return nil, err
	}

	generateTileVal, err := i.Eval("custom.generateTile")
	if err != nil {
		return nil, err
	}

	preAuthFunc := preAuthVal.Interface().(func(*internal.RequestContext, ProviderContext, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (ProviderContext, error))

	generateTileFunc := generateTileVal.Interface().(func(*internal.RequestContext, ProviderContext, internal.TileRequest, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (*internal.Image, error))

	return &Custom{cfg, clientConfig, errorMessages, i, preAuthFunc, generateTileFunc}, nil
}

func (t Custom) PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	return t.preAuthFunc(ctx, providerContext, t.Params, t.clientConfig, t.errorMessages)
}

func (t Custom) GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	img, err := t.generateTileFunc(ctx, providerContext, tileRequest, t.Params, t.clientConfig, t.errorMessages)

	if err != nil {
		return nil, err
	}

	return img, nil
}
