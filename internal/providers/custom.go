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

package providers

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
	Params map[string]interface{} `mapstructure:",remain"`
}

type Custom struct {
	CustomConfig
	clientConfig     *config.ClientConfig
	errorMessages    *config.ErrorMessages
	interp           *interp.Interpreter
	preAuthFunc      func(AuthContext, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (AuthContext, error)
	generateTileFunc func(AuthContext, internal.TileRequest, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (*internal.Image, error)
}

func ConstructCustom(cfg CustomConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages) (*Custom, error) {
	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols)
	i.Use(interp.Exports{
		"tilegroxy/tilegroxy": map[string]reflect.Value{
			"AuthContext":   reflect.ValueOf((*AuthContext)(nil)),
			"TileRequest":   reflect.ValueOf((*internal.TileRequest)(nil)),
			"ClientConfig":  reflect.ValueOf((*config.ClientConfig)(nil)),
			"ErrorMessages": reflect.ValueOf((*config.ErrorMessages)(nil)),
			"Image":         reflect.ValueOf((*internal.Image)(nil)),
			"GetTile":       reflect.ValueOf(getTile),
		}})

	script, err := os.ReadFile(cfg.File)
	if err != nil {
		return nil, err
	}

	_, err = i.Eval(string(script))
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

	preAuthFunc := preAuthVal.Interface().(func(AuthContext, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (AuthContext, error))

	generateTileFunc := generateTileVal.Interface().(func(AuthContext, internal.TileRequest, map[string]interface{}, config.ClientConfig, config.ErrorMessages) (*internal.Image, error))

	return &Custom{cfg, clientConfig, errorMessages, i, preAuthFunc, generateTileFunc}, nil
}

func (t Custom) PreAuth(authContext AuthContext) (AuthContext, error) {
	return t.preAuthFunc(authContext, t.Params, *t.clientConfig, *t.errorMessages)
}

func (t Custom) GenerateTile(authContext AuthContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	img, err := t.generateTileFunc(authContext, tileRequest, t.Params, *t.clientConfig, *t.errorMessages)

	if err != nil {
		return nil, err
	}

	return img, nil
}
