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

package providers

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/entities/layer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Config loading lowercases map keys, so names arrive lowercase here
func Test_CGI_EnvUppercased(t *testing.T) {
	cfg := CGIConfig{
		Exec: "test_files/mapserv_via_docker.sh",
		URI:  "/?x={x}",
		Env:  map[string]string{"foo": "", "mixedinherit": "", "bar": "set"},
	}

	p, err := CGIRegistration{}.Initialize(cfg, layer.ProviderDeps{ClientConfig: testClientConfig, ErrorMessages: testErrMessages})
	require.NoError(t, err)

	h := p.(*CGI).handler
	assert.ElementsMatch(t, []string{"FOO", "MIXEDINHERIT"}, h.InheritEnv)
	assert.Equal(t, []string{"BAR=set"}, h.Env)
}
