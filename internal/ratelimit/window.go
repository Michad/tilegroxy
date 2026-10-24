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
	"math"
	"time"
)

// Covers clock differences between instances so a count isn't dropped just before its window ends
const ttlGrace = time.Second

const daysPerWeek = 7

// Where a rule's counting stands at one moment
type window struct {
	start time.Time
	end   time.Time
	// The previous bucket of a rolling period. Zero for a calendar rule
	prevStart time.Time
	// How much of the previous bucket still falls within the rolling window
	prevWeight float64
	// How long a count in this window must be kept
	ttl time.Duration
}

func (r *rule) windowAt(now time.Time) window {
	now = now.UTC()

	if r.period > 0 {
		start := time.Unix(0, now.UnixNano()-now.UnixNano()%int64(r.period)).UTC()
		elapsed := now.Sub(start)

		return window{
			start:      start,
			end:        start.Add(r.period),
			prevStart:  start.Add(-r.period),
			prevWeight: 1 - float64(elapsed)/float64(r.period),
			// Still needed as the previous bucket throughout the next one
			ttl: start.Add(2*r.period).Sub(now) + ttlGrace,
		}
	}

	var start, end time.Time

	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	switch r.calendar {
	case CalendarHour:
		start = now.Truncate(time.Hour)
		end = start.Add(time.Hour)
	case CalendarDay:
		start = day
		end = start.AddDate(0, 0, 1)
	case CalendarWeek:
		sinceMonday := (int(now.Weekday()) + daysPerWeek - 1) % daysPerWeek
		start = day.AddDate(0, 0, -sinceMonday)
		end = start.AddDate(0, 0, daysPerWeek)
	case CalendarMonth:
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, 0)
	default:
		start = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(1, 0, 0)
	}

	return window{start: start, end: end, ttl: end.Sub(now) + ttlGrace}
}

func (w window) rolling() bool {
	return !w.prevStart.IsZero()
}

// The rolling estimate counts the part of the previous bucket still inside the window
func (w window) used(cur, prev int64) int64 {
	if !w.rolling() {
		return cur
	}

	return cur + int64(math.Floor(float64(prev)*w.prevWeight))
}

// How long until one more request fits under limit, assuming nothing else is counted meanwhile
func (w window) retryAfter(now time.Time, cur, prev, limit int64) time.Duration {
	if !w.rolling() {
		return w.end.Sub(now)
	}

	period := w.end.Sub(w.start)
	// Previous counts may total up to this much once weighted
	allowance := float64(limit - cur)

	if allowance > 0 && prev > 0 {
		wait := time.Duration(float64(period)*(1-allowance/float64(prev))) - now.Sub(w.start)
		if wait < w.end.Sub(now) {
			return wait
		}
	}

	// The current bucket becomes the previous one, weighted down as the next bucket passes
	next := time.Duration(float64(period) * (1 - float64(limit)/float64(cur)))

	return w.end.Sub(now) + max(next, 0)
}

// Rounded up to whole seconds, at least one
func ceilSeconds(d time.Duration) int64 {
	return max(int64(math.Ceil(d.Seconds())), 1)
}
