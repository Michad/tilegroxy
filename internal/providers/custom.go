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
	iso    *v8.Isolate
	global *v8.ObjectTemplate
	fun    *v8.Function
}

func ConstructCustom(config CustomConfig, ErrorMessages *config.ErrorMessages) (*Custom, error) {
	iso := v8.NewIsolate()

	global := v8.NewObjectTemplate(iso)

	printfn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		fmt.Printf("%+v\n", info.Args())
		return nil
	})
	global.Set("print", printfn, v8.ReadOnly)
	ctx := v8.NewContext(iso, global)
	ctx.RunScript("print('foo', 'bar', 0, 1)", "script.js")

	body, err := os.ReadFile(config.File)
	if err != nil {
		return nil, err
	}

	_, err = ctx.RunScript("print('foo', 'bar', 0, 1)", "")
	if err != nil {
		return nil, err
	}

	val, err := ctx.RunScript(string(body), "")
	fmt.Printf("Val: %v\n", val)

	if err != nil {
		return nil, err
	}

	tileFuncObj, err := ctx.Global().Get("GenerateTile2")
	fmt.Printf("%v\n", tileFuncObj)

	if err != nil {
		return nil, err
	}

	tileFunc, err := tileFuncObj.AsFunction()
	if err != nil {
		log.Printf("Err0: %v\n", err)
		return nil, err
	}

	return &Custom{config, iso, global, tileFunc}, nil
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

	fetchfn := v8.NewFunctionTemplate(t.iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		url := args[0].String()
		fmt.Printf("URL: %v\n", url)

		img, err := getTile(clientConfig, url, map[string]string{})

		var val *v8.Value

		if err != nil {
			val, _ = v8.NewValue(t.iso, err.Error())
		}

		if img != nil {
			val, _ = v8.NewValue(t.iso, string(*img))
		}

		return val
	})
	err := t.global.Set("fetch", fetchfn)
	if err != nil {
		return nil, err
	}

	ctx := v8.NewContext(t.iso, t.global)
	ctx.RunScript("print('foo', 'bar', 0, 1)", "")

	body, err := os.ReadFile(t.File)
	if err != nil {
		return nil, err
	}

	_, err = ctx.RunScript("print('foo', 'bar', 0, 1)", "")
	if err != nil {
		return nil, err
	}

	_, err = ctx.RunScript(string(body), "")
	if err != nil {
		return nil, err
	}

	paramBytes, err := json.Marshal(t.CustomConfig.Params)
	if err != nil {
		log.Printf("Err1: %v\n", err)
		return nil, err
	}

	paramVal, err := v8.JSONParse(ctx, string(paramBytes))
	if err != nil {
		log.Printf("Err2: %v\n", err)
		return nil, err
	}

	authBytes, err := json.Marshal(authContext)
	if err != nil {
		log.Printf("Err3: %v\n", err)
		return nil, err
	}

	authVal, err := v8.JSONParse(ctx, string(authBytes))
	if err != nil {
		log.Printf("Err4: %v\n", err)
		return nil, err
	}

	clientBytes, err := json.Marshal(clientConfig)
	if err != nil {
		log.Printf("Err5: %v\n", err)
		return nil, err
	}

	clientVal, err := v8.JSONParse(ctx, string(clientBytes))
	if err != nil {
		log.Printf("Err6: %v\n", err)
		return nil, err
	}

	errorBytes, err := json.Marshal(errorMessages)
	if err != nil {
		log.Printf("Err7: %v\n", err)
		return nil, err
	}

	errorVal, err := v8.JSONParse(ctx, string(errorBytes))
	if err != nil {
		log.Printf("Err8: %v\n", err)
		return nil, err
	}

	tileBytes, err := json.Marshal(tileRequest)
	if err != nil {
		log.Printf("Err9: %v\n", err)
		return nil, err
	}

	tileVal, err := v8.JSONParse(ctx, string(tileBytes))
	if err != nil {
		log.Printf("Err10: %v\n", err)
		return nil, err
	}

	tileFuncObj, err := ctx.Global().Get("GenerateTile2")
	log.Printf("%v\n", tileFuncObj)
	if err != nil {
		log.Printf("Err11: %v\n", err)
		return nil, err
	}

	tileFunc, err := tileFuncObj.AsFunction()
	if err != nil {
		log.Printf("Err12: %v\n", err)
		return nil, err
	}

	val, err := tileFunc.Call(ctx.Global(), authVal, paramVal, clientVal, errorVal, tileVal)

	if err != nil {
		log.Printf("Err: %v\n", err)
		return nil, err
	}

	log.Printf("response: %v\n", val)

	return nil, nil
}
