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

package authentications

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/stretchr/testify/require"
)

// A typo'd "statickey" (the real field is "Key") used to silently decode to a zero-value
// StaticKeyConfig, which static_key.go then filled with a random key - locking every client out
// of a config that passed validation. Strict decoding should catch the typo at construction time.
func Test_StaticKey_TypoedFieldNameErrors(t *testing.T) {
	rawConfig := map[string]interface{}{
		"name":      "static key",
		"statickey": "mysecret",
	}

	_, err := authentication.ConstructAuth(rawConfig, config.DefaultConfig().Error.Messages)

	require.Error(t, err)
}

func Test_StaticKey_CorrectFieldNameWorks(t *testing.T) {
	rawConfig := map[string]interface{}{
		"name": "static key",
		"key":  "mysecret",
	}

	auth, err := authentication.ConstructAuth(rawConfig, config.DefaultConfig().Error.Messages)

	require.NoError(t, err)
	require.NotNil(t, auth)
}
