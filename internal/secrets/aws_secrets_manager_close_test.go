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

//go:build !no_aws

package secrets

import (
	"context"
	"testing"
	"time"

	"github.com/maypok86/otter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Otter's cleanup and process goroutines only exit once the cache is closed, so a hot reload leaks a
// pair per generation without this. Close clears the cache synchronously, which is what's observable
func Test_SecretManager_CloseReleasesCache(t *testing.T) {
	cache, err := otter.MustBuilder[string, string](cacheSize).WithTTL(time.Hour).Build()
	require.NoError(t, err)
	cache.Set("k", "v")

	s := AWSSecretsManager{cache: &cache}
	require.NoError(t, s.Close(context.Background()))

	_, ok := cache.Get("k")
	assert.False(t, ok)
}

func Test_SecretManager_CloseIsIdempotent(t *testing.T) {
	cache, err := otter.MustBuilder[string, string](cacheSize).WithTTL(time.Hour).Build()
	require.NoError(t, err)

	s := AWSSecretsManager{cache: &cache}
	require.NoError(t, s.Close(context.Background()))
	require.NoError(t, s.Close(context.Background()))
}

func Test_SecretManager_CloseWithoutCache(t *testing.T) {
	s := AWSSecretsManager{}

	require.NoError(t, s.Close(context.Background()))
}
