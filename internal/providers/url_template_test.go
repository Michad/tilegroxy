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

//go:build !unit

package providers

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_UrlTemplateValidate(t *testing.T) {
	p, err := URLTemplateRegistration{}.Initialize(URLTemplateConfig{}, config.ClientConfig{}, testErrMessages, nil, nil)

	assert.Nil(t, p)
	require.Error(t, err)

	var clientConfig = config.ClientConfig{StatusCodes: []int{400}, MaxLength: 2000, ContentTypes: []string{"image/png"}, UnknownLength: true}
	p, err = URLTemplateRegistration{}.Initialize(URLTemplateConfig{Template: "url here"}, clientConfig, testErrMessages, nil, nil)
	assert.NotNil(t, p)
	require.NoError(t, err)
}
