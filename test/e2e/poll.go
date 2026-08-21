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

//go:build e2e

package e2e

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// How often Until rechecks its condition.
const pollInterval = 50 * time.Millisecond

// Scale stretches a timeout by TILEGROXY_E2E_TIMEOUT_SCALE so CI can be slower than a laptop
// without every deadline being tuned tight.
func Scale(d time.Duration) time.Duration {
	raw := os.Getenv("TILEGROXY_E2E_TIMEOUT_SCALE")
	if raw == "" {
		return d
	}

	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f <= 0 {
		return d
	}

	return time.Duration(float64(d) * f)
}

// Until polls cond until it returns true or the timeout expires. Waiting is always a poll with a
// deadline, never a fixed sleep, so a slow machine is slow rather than flaky.
func Until(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(Scale(timeout))

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(pollInterval)
	}

	t.Fatalf("timed out after %v waiting for %s", Scale(timeout), desc)
}
