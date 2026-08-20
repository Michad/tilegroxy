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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_BinaryPath_ResolvesExistingBinary(t *testing.T) {
	path := BinaryPath(t)

	info, err := os.Stat(path)
	require.NoError(t, err, "BinaryPath must return a path that exists")
	assert.False(t, info.IsDir())
}

func Test_BinaryPath_HonorsEnvOverride(t *testing.T) {
	tmp := t.TempDir() + "/fake-tilegroxy"
	require.NoError(t, os.WriteFile(tmp, []byte("#!/bin/sh\n"), 0700))
	t.Setenv("TILEGROXY_E2E_BINARY", tmp)

	assert.Equal(t, tmp, BinaryPath(t))
}

func Test_Scale_AppliesMultiplier(t *testing.T) {
	t.Setenv("TILEGROXY_E2E_TIMEOUT_SCALE", "2")
	assert.Equal(t, 10*time.Second, Scale(5*time.Second))
}

func Test_Scale_DefaultsToUnscaled(t *testing.T) {
	t.Setenv("TILEGROXY_E2E_TIMEOUT_SCALE", "")
	assert.Equal(t, 5*time.Second, Scale(5*time.Second))
}
