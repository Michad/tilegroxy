// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testutil

import (
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_FreePort_IsBindable(t *testing.T) {
	port := FreePort(t)

	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err)
	require.NoError(t, l.Close())
}

// Enough calls that the kernel would repeat a recent port without the dedupe
func Test_FreePort_NeverRepeats(t *testing.T) {
	const n = 500

	var mu sync.Mutex
	var wg sync.WaitGroup
	seen := make(map[int]struct{}, n)

	for range n {
		wg.Go(func() {
			port := FreePort(t)

			mu.Lock()
			defer mu.Unlock()

			_, dup := seen[port]
			assert.False(t, dup, "port %d handed out twice", port)
			seen[port] = struct{}{}
		})
	}

	wg.Wait()

	assert.Len(t, seen, n)
}
