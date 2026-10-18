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

package analytics

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Gives registrations something to construct
type fake struct{}

func (f *fake) Record(_ context.Context, _ Event) error {
	return nil
}

type fakeRegistration struct{}

func (s fakeRegistration) InitializeConfig() any { return struct{}{} }
func (s fakeRegistration) Name() string          { return "testfake" }

func (s fakeRegistration) Initialize(_ any, _ AnalyticsDeps) (Analytics, error) {
	return &fake{}, nil
}

func Test_RegisteredAnalyticsNames(t *testing.T) {
	RegisterAnalytics(fakeRegistration{})

	assert.Contains(t, RegisteredAnalyticsNames(), "testfake")

	_, ok := RegisteredAnalytics("testfake")
	assert.True(t, ok)

	_, ok = RegisteredAnalytics("nope")
	assert.False(t, ok)
}

// Registers distinct names, unlike fakeRegistration which is always "testfake"
type namedFakeRegistration struct {
	name string
}

func (s namedFakeRegistration) Name() string          { return s.name }
func (s namedFakeRegistration) InitializeConfig() any { return struct{}{} }
func (s namedFakeRegistration) Initialize(_ any, _ AnalyticsDeps) (Analytics, error) {
	return &fake{}, nil
}

func Test_AnalyticsRegistry_ConcurrentRegistrationIsRaceFree(t *testing.T) {
	var wg sync.WaitGroup
	const n = 50

	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			RegisterAnalytics(namedFakeRegistration{name: fmt.Sprintf("stub-concurrent-%d", i)})
		}(i)
	}

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = RegisteredAnalyticsNames()
		}()
	}

	wg.Wait()

	for i := range n {
		_, ok := RegisteredAnalytics(fmt.Sprintf("stub-concurrent-%d", i))
		assert.True(t, ok)
	}
}
