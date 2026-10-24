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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type clockStore struct {
	*Memory
	clock  *testClock
	offset time.Duration
	err    error
}

func (s *clockStore) Now(_ context.Context) (time.Time, error) {
	return s.clock.now().Add(s.offset), s.err
}

const clockConfig = `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [], limit: 5, period: 60}
layers:
  - id: l
    provider: {name: static, color: FFF}
`

func newClockLimiter(t *testing.T, offset time.Duration) (*Limiter, *testClock, *clockStore) {
	t.Helper()

	l, clock := newTestLimiter(t, clockConfig)
	store := &clockStore{Memory: l.store.(*Memory), clock: clock, offset: offset}
	l.store = store
	l.initClock(context.Background())

	return l, clock, store
}

func Test_Clock_WindowsFollowStore(t *testing.T) {
	l, _, _ := newClockLimiter(t, 45*time.Second)

	res := l.Check(requestContext("", "", "10.0.0.1:1"), "l")

	require.Equal(t, Allowed, res.Outcome)
	assert.Contains(t, header(res, HeaderRateLimit), ";r=4;t=15")
}

func Test_Clock_LocalWithoutStoreClock(t *testing.T) {
	l, clock := newTestLimiter(t, clockConfig)
	l.initClock(context.Background())

	assert.Nil(t, l.clock)
	assert.Equal(t, clock.now(), l.windowNow())
}

func Test_Clock_Resyncs(t *testing.T) {
	l, clock, store := newClockLimiter(t, time.Second)

	assert.Equal(t, clock.now().Add(time.Second), l.windowNow())

	store.offset = -time.Second
	clock.advance(clockSyncInterval)

	l.windowNow()

	require.Eventually(t, func() bool {
		return l.clockOffset.Load() == int64(-time.Second)
	}, time.Second, time.Millisecond)
}

func Test_Clock_FailureKeepsOffset(t *testing.T) {
	l, _, store := newClockLimiter(t, time.Second)

	store.err = errors.New("down")
	l.syncClock(context.Background())

	assert.Equal(t, int64(time.Second), l.clockOffset.Load())
}
