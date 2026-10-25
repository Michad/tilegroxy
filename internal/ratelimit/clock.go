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
	"fmt"
	"log/slog"
	"time"

	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
)

// How often the offset from a shared store's clock is measured again
const clockSyncInterval = time.Minute

// The time windows are computed from. The shared store's clock when it has one, so every instance agrees
func (l *Limiter) windowNow() time.Time {
	now := l.now()

	if l.clock == nil {
		return now
	}

	last := l.lastSync.Load()

	if now.UnixNano()-last >= int64(clockSyncInterval) && l.lastSync.CompareAndSwap(last, now.UnixNano()) {
		go l.syncClock(context.Background())
	}

	return now.Add(time.Duration(l.clockOffset.Load()))
}

// Assumes the store read its clock halfway through the round trip. Keeps the previous offset on failure
func (l *Limiter) syncClock(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	before := l.now()

	storeNow, err := l.clock.Now(ctx)
	if err != nil {
		slog.WarnContext(ctx, fmt.Sprintf("Unable to read the clock of rate limit store %v, so windows may not line up between instances: %v", l.storeName, err))
		return
	}

	after := l.now()
	local := before.Add(after.Sub(before) / 2)

	l.clockOffset.Store(int64(storeNow.Sub(local)))
	l.lastSync.Store(after.UnixNano())
}

func (l *Limiter) initClock(ctx context.Context) {
	clock, ok := l.store.(ratelimit.Clock)
	if !ok || !l.enabled {
		return
	}

	l.clock = clock
	l.syncClock(ctx)
}
