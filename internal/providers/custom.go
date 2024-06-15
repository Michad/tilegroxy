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
	// "errors"
	"log"
	"os"

	// "time"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"

	"github.com/robertkrimen/otto"
)

type CustomConfig struct {
	File   string
	Params map[string]interface{} `mapstructure:",remain"`
}

type Custom struct {
	CustomConfig
	vm *otto.Otto
}

func ConstructCustom(config CustomConfig, ErrorMessages *config.ErrorMessages) (*Custom, error) {
	vm := otto.New()

	body, err := os.ReadFile(config.File)
	if err != nil {
		return nil, err
	}

	_, err = vm.Run(body)

	if err != nil {
		return nil, err
	}

	return &Custom{config, vm}, nil
}

func (t Custom) PreAuth(authContext *AuthContext) error {
	// val, err := t.vm.Call("PreAuth", nil, authContext)

	// if err != nil {
	// 	return err
	// }

	// if val.IsNull() {
	// 	return nil
	// }

	// if !val.IsObject() {
	// 	return errors.New("CUSTOM: invalid return")
	// }
	// valObj := val.Object()

	// return nil

	// expVal, err := valObj.Get("expiration")
	// if err != nil {
	// 	return err
	// }

	// if !expVal.IsNull() {
	// 	expInt, err := expVal.ToInteger()
	// 	if err != nil {
	// 		return err
	// 	}
	// 	authContext.Expiration = time.Unix(expInt, 0)
	// }

	//TODO: rest of authcontext

	return nil
}

func (t Custom) GenerateTile(authContext *AuthContext, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages, tileRequest internal.TileRequest) (*internal.Image, error) {
	val, err := t.vm.Call("GenerateTile", nil, authContext, t.CustomConfig.Params, clientConfig, errorMessages, tileRequest)

	if err != nil {
		return nil, err
	}

	log.Printf("response: %v\n", val)

	return nil, nil
}
