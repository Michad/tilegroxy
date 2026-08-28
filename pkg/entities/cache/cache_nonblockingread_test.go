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

package cache

import (
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
)

// nonblockingread is handled generically in ConstructCache rather than by each cache's own Config
// struct (stubCacheRegistration's is an empty struct{}), so it must be stripped before decoding or
// DecodeEntityConfig's ErrorUnused check would reject it as an unrecognized key.
func Test_ConstructCache_NonBlockingReadDefaultsFalse(t *testing.T) {
	RegisterCache(stubCacheRegistration{name: "stub-nbr-default"})

	c, err := ConstructCache(map[string]interface{}{"name": "stub-nbr-default"}, CacheDeps{})
	require.NoError(t, err)

	wrapper, ok := c.(CacheWrapper)
	require.True(t, ok)
	require.False(t, wrapper.NonBlockingRead)
}

func Test_ConstructCache_NonBlockingReadTrue(t *testing.T) {
	RegisterCache(stubCacheRegistration{name: "stub-nbr-true"})

	c, err := ConstructCache(map[string]interface{}{"name": "stub-nbr-true", "nonblockingread": true}, CacheDeps{})
	require.NoError(t, err)

	wrapper, ok := c.(CacheWrapper)
	require.True(t, ok)
	require.True(t, wrapper.NonBlockingRead)
}

func Test_ConstructCache_NonBlockingReadDoesNotLeakIntoEntityConfig(t *testing.T) {
	RegisterCache(stubCacheRegistration{name: "stub-nbr-strip"})

	// stubCacheRegistration.InitializeConfig() is an empty struct{}, so if nonblockingread weren't
	// stripped first, DecodeEntityConfig's ErrorUnused check would fail this.
	_, err := ConstructCache(map[string]interface{}{"name": "stub-nbr-strip", "nonblockingread": false}, CacheDeps{})
	require.NoError(t, err)
}

func Test_ConstructCache_NonBlockingReadRejectsNonBool(t *testing.T) {
	RegisterCache(stubCacheRegistration{name: "stub-nbr-badtype"})
	errorMessages := config.DefaultConfig().Error.Messages

	_, err := ConstructCache(map[string]interface{}{"name": "stub-nbr-badtype", "nonblockingread": "yes"}, CacheDeps{ErrorMessages: errorMessages})
	require.Error(t, err)
}
