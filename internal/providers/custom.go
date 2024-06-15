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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"

	// "time"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"

	v8 "rogchap.com/v8go"
)

type CustomConfig struct {
	File   string
	Params map[string]interface{} `mapstructure:",remain"`
}

type Custom struct {
	CustomConfig
	clientConfig     *config.ClientConfig
	errorMessages    *config.ErrorMessages
	ctx              *v8.Context
	preAuthFunc      *v8.Function
	generateTileFunc *v8.Function
}

func ConstructCustom(config CustomConfig, clientConfig *config.ClientConfig, errorMessages *config.ErrorMessages) (*Custom, error) {
	iso := v8.NewIsolate()

	global := v8.NewObjectTemplate(iso)

	printfn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		fmt.Printf("%+v\n", info.Args())
		return nil
	})
	err := global.Set("print", printfn, v8.ReadOnly)
	if err != nil {
		return nil, err
	}

	fetchfn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		url := args[0].String()
		fmt.Printf("URL: %v\n", url)

		img, err := getTile(clientConfig, url, map[string]string{})

		var val *v8.Value

		if err != nil {
			val, _ = v8.NewValue(iso, err.Error())
		}

		if img != nil {
			val, _ = v8.NewValue(iso, *img)
		}

		return val
	})
	err = global.Set("fetch", fetchfn, v8.ReadOnly)
	if err != nil {
		return nil, err
	}

	ctx := v8.NewContext(iso, global)

	body, err := os.ReadFile(config.File)
	if err != nil {
		return nil, err
	}

	_, err = ctx.RunScript(string(body), "")

	if err != nil {
		return nil, err
	}

	preAuthFuncObj, err := ctx.Global().Get("PreAuth")

	if err != nil {
		return nil, err
	}

	tileFuncObj, err := ctx.Global().Get("GenerateTile")

	if err != nil {
		return nil, err
	}

	preAuthFunc, err := preAuthFuncObj.AsFunction()
	if err != nil {
		return nil, err
	}

	tileFunc, err := tileFuncObj.AsFunction()
	if err != nil {
		return nil, err
	}

	return &Custom{config, clientConfig, errorMessages, ctx, preAuthFunc, tileFunc}, nil
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

func (t Custom) GenerateTile(authContext *AuthContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	paramBytes, err1 := json.Marshal(t.CustomConfig.Params)
	paramVal, err2 := v8.JSONParse(t.ctx, string(paramBytes))
	authBytes, err3 := json.Marshal(authContext)
	authVal, err4 := v8.JSONParse(t.ctx, string(authBytes))
	tileBytes, err5 := json.Marshal(tileRequest)
	tileVal, err6 := v8.JSONParse(t.ctx, string(tileBytes))

	if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
		return nil, err
	}

	val, err := t.generateTileFunc.Call(t.ctx.Global(), authVal, paramVal, tileVal)

	if err != nil {
		log.Printf("Err: %v\n", err)
		return nil, err
	}
	val.SharedArrayBufferGetContents()

	valStr := []byte(val.String())

	log.Printf("response: %v\n", valStr)

	return &valStr, nil
}
