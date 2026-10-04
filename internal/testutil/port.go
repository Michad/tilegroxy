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

// Package testutil holds helpers shared by tests across packages. Nothing outside of tests imports it.
package testutil

import (
	"context"
	"net"
	"sync"
	"testing"
)

const maxPortAttempts = 100

var (
	handedOutMu sync.Mutex
	handedOut   = map[int]struct{}{}
)

// FreePort asks the kernel for an unused TCP port on 127.0.0.1 and releases it for the caller to bind.
// Nothing reserves a released port, so the kernel can return it again before the first caller binds.
// Ports already handed out by this process are skipped to stop two servers in one test run racing for one port.
func FreePort(t testing.TB) int {
	t.Helper()

	handedOutMu.Lock()
	defer handedOutMu.Unlock()

	for range maxPortAttempts {
		port, err := listenAndRelease()
		if err != nil {
			t.Fatalf("cannot allocate a port: %v", err)
		}

		if _, seen := handedOut[port]; seen {
			continue
		}

		handedOut[port] = struct{}{}

		return port
	}

	t.Fatalf("kernel kept returning ports already handed out after %d attempts", maxPortAttempts)

	return 0
}

func listenAndRelease() (int, error) {
	var lc net.ListenConfig

	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}

	port := l.Addr().(*net.TCPAddr).Port

	return port, l.Close()
}
