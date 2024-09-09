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

package checks

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Fail(t *testing.T) {
	cfgAll := config.DefaultConfig()
	msg := cfgAll.Error.Messages

	cacheReg := caches.NoopRegistration{}
	cacheCfg := cacheReg.InitializeConfig()
	cache, err := cacheReg.Initialize(cacheCfg, msg)
	require.NoError(t, err)

	reg := CacheCheckRegistration{}
	cfgAny := reg.InitializeConfig()
	hc, err := reg.Initialize(cfgAny, nil, cache, &cfgAll)
	require.NoError(t, err)

	err = hc.Check(context.Background())
	assert.Error(t, err)
}

func Test_Works(t *testing.T) {
	cfgAll := config.DefaultConfig()
	msg := cfgAll.Error.Messages

	cacheReg := caches.MemoryRegistration{}
	cacheCfg := cacheReg.InitializeConfig()
	cache, err := cacheReg.Initialize(cacheCfg, msg)
	require.NoError(t, err)

	reg := CacheCheckRegistration{}
	cfgAny := reg.InitializeConfig()
	hc, err := reg.Initialize(cfgAny, nil, cache, &cfgAll)
	require.NoError(t, err)

	require.IsType(t, &CacheCheck{}, hc)
	cc := hc.(*CacheCheck)
	assert.Equal(t, uint(600), cc.Delay)
	assert.Equal(t, cc.Delay, cc.GetDelay())

	err = hc.Check(context.Background())
	assert.NoError(t, err)
}
