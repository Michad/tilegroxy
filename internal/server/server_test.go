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

package server

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
)

func Test_ListenAndServe_Validate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Encrypt = &config.EncryptionConfig{Certificate: "asfjaslkf", Domain: ""}

	require.Error(t, ListenAndServe(&cfg, nil, nil, nil))
}
