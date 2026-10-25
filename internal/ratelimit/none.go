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

	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
)

type NoneConfig struct{}

// Never called, since the limiter treats this store as disabled
type None struct{}

func init() {
	ratelimit.RegisterRateLimiter(NoneRegistration{})
}

type NoneRegistration struct{}

func (NoneRegistration) InitializeConfig() any {
	return NoneConfig{}
}

func (NoneRegistration) Name() string {
	return noneName
}

func (NoneRegistration) Initialize(_ any, _ ratelimit.RateLimiterDeps) (ratelimit.RateLimiter, error) {
	return None{}, nil
}

func (None) Get(_ context.Context, counters []ratelimit.RateLimitCounter) ([]int64, error) {
	return make([]int64, len(counters)), nil
}

func (None) Increment(_ context.Context, _ []ratelimit.RateLimitCounter) error {
	return nil
}
