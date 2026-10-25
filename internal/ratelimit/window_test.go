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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_Window_Rolling(t *testing.T) {
	r := &rule{period: time.Minute}
	now := time.Date(2026, 10, 24, 12, 30, 15, 0, time.UTC)

	w := r.windowAt(now)

	assert.Equal(t, time.Date(2026, 10, 24, 12, 30, 0, 0, time.UTC), w.start)
	assert.Equal(t, time.Date(2026, 10, 24, 12, 31, 0, 0, time.UTC), w.end)
	assert.Equal(t, time.Date(2026, 10, 24, 12, 29, 0, 0, time.UTC), w.prevStart)
	assert.InDelta(t, 0.75, w.prevWeight, 0.0001)
	assert.Equal(t, 105*time.Second+ttlGrace, w.ttl)
	assert.True(t, w.rolling())

	// The docs' example: 15 seconds in, 45/60 of the previous bucket counts
	assert.Equal(t, int64(10+30), w.used(10, 40))
}

func Test_Window_Calendar(t *testing.T) {
	// A Saturday
	now := time.Date(2026, 10, 24, 12, 30, 15, 0, time.UTC)

	cases := map[string][2]time.Time{
		CalendarHour:  {time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 24, 13, 0, 0, 0, time.UTC)},
		CalendarDay:   {time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)},
		CalendarWeek:  {time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)},
		CalendarMonth: {time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		CalendarYear:  {time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	for calendar, want := range cases {
		w := (&rule{calendar: calendar}).windowAt(now)

		assert.Equal(t, want[0], w.start, calendar)
		assert.Equal(t, want[1], w.end, calendar)
		assert.False(t, w.rolling(), calendar)
		assert.Equal(t, want[1].Sub(now)+ttlGrace, w.ttl, calendar)
		assert.Equal(t, int64(7), w.used(7, 100), calendar)
	}
}

func Test_Window_WeekStartsMonday(t *testing.T) {
	sunday := time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), (&rule{calendar: CalendarWeek}).windowAt(sunday).start)
	assert.Equal(t, monday, (&rule{calendar: CalendarWeek}).windowAt(monday).start)
}

func Test_Window_CalendarUsesUTC(t *testing.T) {
	local := time.FixedZone("UTC-5", -5*60*60)
	now := time.Date(2026, 10, 31, 22, 0, 0, 0, local)

	w := (&rule{calendar: CalendarMonth}).windowAt(now)

	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), w.start)
}

func Test_Window_RetryAfterCalendar(t *testing.T) {
	now := time.Date(2026, 10, 31, 23, 59, 30, 0, time.UTC)
	w := (&rule{calendar: CalendarMonth}).windowAt(now)

	assert.Equal(t, 30*time.Second, w.retryAfter(now, 100, 0, 100))
}

func Test_Window_RetryAfterWithinBucket(t *testing.T) {
	r := &rule{period: 10 * time.Second}
	now := time.Unix(1000, 0)
	w := r.windowAt(now)

	// At the start of a bucket the whole previous bucket counts. 2 + floor(10*w) < 5 needs w < 0.3
	assert.Equal(t, 7*time.Second, w.retryAfter(now, 2, 10, 5))
}

func Test_Window_RetryAfterNextBucket(t *testing.T) {
	r := &rule{period: 10 * time.Second}
	now := time.Unix(1002, 0)
	w := r.windowAt(now)

	// 8 left in this bucket, then floor(10*w) < 5 needs w < 0.5 so 5 more
	assert.Equal(t, 13*time.Second, w.retryAfter(now, 10, 0, 5))

	// Only the last sliver of a large previous bucket is still too much
	assert.InDelta(t, 7.99, w.retryAfter(now, 4, 1000, 5).Seconds(), 0.001)
}

func Test_CeilSeconds(t *testing.T) {
	assert.Equal(t, int64(1), ceilSeconds(0))
	assert.Equal(t, int64(1), ceilSeconds(time.Millisecond))
	assert.Equal(t, int64(2), ceilSeconds(1001*time.Millisecond))
}
