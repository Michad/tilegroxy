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
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
)

type stubCache struct{}

func (stubCache) Lookup(_ context.Context, _ pkg.TileRequest) (*pkg.Image, error) { return nil, nil }
func (stubCache) Save(_ context.Context, _ pkg.TileRequest, _ *pkg.Image) error   { return nil }

type stubCacheRegistration struct {
	name string
}

func (s stubCacheRegistration) Name() string          { return s.name }
func (s stubCacheRegistration) InitializeConfig() any { return struct{}{} }
func (s stubCacheRegistration) Initialize(_ any, _ config.ErrorMessages) (Cache, error) {
	return stubCache{}, nil
}

func Test_CacheRegistry_ConcurrentRegistrationIsRaceFree(t *testing.T) {
	var wg sync.WaitGroup
	const n = 50

	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			RegisterCache(stubCacheRegistration{name: fmt.Sprintf("stub-concurrent-%d", i)})
		}(i)
	}

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = RegisteredCacheNames()
		}()
	}

	wg.Wait()

	for i := range n {
		_, ok := RegisteredCache(fmt.Sprintf("stub-concurrent-%d", i))
		require.True(t, ok)
	}
}
