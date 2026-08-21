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
	"path/filepath"
	"runtime"
	"testing"
)

// BinaryPath resolves the binary under test. TILEGROXY_E2E_BINARY points at an arbitrary build
// (a release artifact, or one extracted from the Docker image); otherwise it is the repo-root
// binary that `make build` produces.
func BinaryPath(t *testing.T) string {
	t.Helper()

	if env := os.Getenv("TILEGROXY_E2E_BINARY"); env != "" {
		if _, err := os.Stat(env); err != nil {
			t.Fatalf("TILEGROXY_E2E_BINARY is set to %q but it cannot be read: %v", env, err)
		}

		return env
	}

	path := filepath.Join(repoRoot(t), "tilegroxy")

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no binary at %v. Run `make e2e`, which builds it first, or set TILEGROXY_E2E_BINARY", path)
	}

	return path
}

// repoRoot locates the repository root from this source file's own path, so tests do not depend on
// the working directory the runner chose.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller path")
	}

	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}
