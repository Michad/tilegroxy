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
	"errors"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

var testErrMessages = config.DefaultConfig().Error.Messages
var testClientConfig = config.DefaultConfig().Client

type FailConfig struct {
	OnAuth  bool
	Message string
}

type Fail struct {
	FailConfig
}

func ConstructFail(config FailConfig, clientConfig config.ClientConfig, errorMessages config.ErrorMessages) (*Fail, error) {
	return &Fail{config}, nil
}

func (t Fail) PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	if t.OnAuth {
		return providerContext, errors.New(t.Message)
	}
	return providerContext, nil
}

func (t Fail) GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	return nil, errors.New(t.Message)
}
