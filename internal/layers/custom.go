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

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities"

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
	preAuthFunc      func(*pkg.RequestContext, entities.ProviderContext, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (entities.ProviderContext, error)
	generateTileFunc func(*pkg.RequestContext, entities.ProviderContext, pkg.TileRequest, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (*pkg.Image, error)
}

func init() {
	entities.RegisterProvider(CustomRegistration{})
}

type CustomRegistration struct {
}

func (s CustomRegistration) InitializeConfig() any {
	return CustomConfig{}
}

func (s CustomRegistration) Name() string {
	return "custom"
}

func (s CustomRegistration) Initialize(cfgAny any, clientConfig config.ClientConfig, errorMessages config.ErrorMessages) (entities.Provider, error) {
	cfg := cfgAny.(CustomConfig)
	i := interp.New(interp.Options{Unrestricted: true})
	i.Use(stdlib.Symbols)
	i.Use(interp.Exports{
		"tilegroxy/tilegroxy": map[string]reflect.Value{
			"RequestContext":  reflect.ValueOf((*pkg.RequestContext)(nil)),
			"ProviderContext": reflect.ValueOf((*entities.ProviderContext)(nil)),
			"TileRequest":     reflect.ValueOf((*pkg.TileRequest)(nil)),
			"ClientConfig":    reflect.ValueOf((*config.ClientConfig)(nil)),
			"ErrorMessages":   reflect.ValueOf((*config.ErrorMessages)(nil)),
			"Image":           reflect.ValueOf((*pkg.Image)(nil)),
			"AuthError":       reflect.ValueOf((*pkg.AuthError)(nil)),
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

	preAuthFunc := preAuthVal.Interface().(func(*pkg.RequestContext, entities.ProviderContext, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (entities.ProviderContext, error))

	generateTileFunc := generateTileVal.Interface().(func(*pkg.RequestContext, entities.ProviderContext, pkg.TileRequest, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (*pkg.Image, error))

	return &Custom{cfg, clientConfig, errorMessages, i, preAuthFunc, generateTileFunc}, nil
}

func (t Custom) PreAuth(ctx *pkg.RequestContext, providerContext entities.ProviderContext) (entities.ProviderContext, error) {
	return t.preAuthFunc(ctx, providerContext, t.Params, t.clientConfig, t.errorMessages)
}

func (t Custom) GenerateTile(ctx *pkg.RequestContext, providerContext entities.ProviderContext, tileRequest pkg.TileRequest) (*pkg.Image, error) {
	img, err := t.generateTileFunc(ctx, providerContext, tileRequest, t.Params, t.clientConfig, t.errorMessages)

	if err != nil {
		return nil, err
	}

	return img, nil
}
