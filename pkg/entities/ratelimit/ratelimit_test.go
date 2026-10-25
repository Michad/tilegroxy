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

package ratelimit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fake struct{}

func (fake) Get(_ context.Context, counters []RateLimitCounter) ([]int64, error) {
	return make([]int64, len(counters)), nil
}

func (fake) Increment(_ context.Context, _ []RateLimitCounter) error {
	return nil
}

type fakeRegistration struct{}

func (fakeRegistration) InitializeConfig() any { return struct{}{} }
func (fakeRegistration) Name() string          { return "testfake" }

func (fakeRegistration) Initialize(_ any, _ RateLimiterDeps) (RateLimiter, error) {
	return fake{}, nil
}

func Test_RegisteredStoreNames(t *testing.T) {
	RegisterRateLimiter(fakeRegistration{})

	assert.Contains(t, RegisteredRateLimiterNames(), "testfake")

	_, ok := RegisteredRateLimiter("testfake")
	assert.True(t, ok)

	_, ok = RegisteredRateLimiter("nope")
	assert.False(t, ok)
}
