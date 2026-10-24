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

func Test_LRU_CountsAndExpires(t *testing.T) {
	c := newLRU(10)
	now := time.Unix(1000, 0)

	assert.Equal(t, int64(0), c.get("a", now))
	assert.Equal(t, int64(1), c.add("a", now, time.Second))
	assert.Equal(t, int64(2), c.add("a", now, time.Second))
	assert.Equal(t, int64(2), c.get("a", now.Add(999*time.Millisecond)))
	assert.Equal(t, int64(0), c.get("a", now.Add(time.Second)))

	assert.Equal(t, int64(1), c.add("a", now.Add(2*time.Second), time.Second))
}

func Test_LRU_ExpiredCountStartsOver(t *testing.T) {
	c := newLRU(10)
	now := time.Unix(1000, 0)

	c.add("a", now, time.Second)
	c.add("a", now, time.Second)

	assert.Equal(t, int64(1), c.add("a", now.Add(5*time.Second), time.Second))
}

func Test_LRU_KeepsLongestExpiry(t *testing.T) {
	c := newLRU(10)
	now := time.Unix(1000, 0)

	c.add("a", now, 10*time.Second)
	c.add("a", now, time.Second)

	assert.Equal(t, int64(2), c.get("a", now.Add(5*time.Second)))
}

func Test_LRU_EvictsLeastRecentlyUsed(t *testing.T) {
	c := newLRU(2)
	now := time.Unix(1000, 0)

	c.add("a", now, time.Hour)
	c.add("b", now, time.Hour)
	c.get("a", now)
	c.add("c", now, time.Hour)

	assert.Equal(t, int64(1), c.get("a", now))
	assert.Equal(t, int64(0), c.get("b", now))
	assert.Equal(t, int64(1), c.get("c", now))
}

func Test_LRU_RemoveAndMinimumSize(t *testing.T) {
	c := newLRU(0)
	now := time.Unix(1000, 0)

	c.add("a", now, time.Hour)
	c.add("b", now, time.Hour)
	assert.Equal(t, int64(0), c.get("a", now))

	c.remove("b")
	c.remove("missing")
	assert.Equal(t, int64(0), c.get("b", now))
}
