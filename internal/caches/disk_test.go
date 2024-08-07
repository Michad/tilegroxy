// Copyright 2024 Michael Davis
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

package caches

import (
	"os"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestDisk(t *testing.T) {
	dir, err := os.MkdirTemp("", "tilegroxy-test-disk")
	defer os.RemoveAll(dir)

	require.NoError(t, err)
	cfg := DiskConfig{Path: dir}

	c, err := DiskRegistration{}.Initialize(cfg, config.ErrorMessages{})
	require.NoError(t, err)
	validateSaveAndLookup(t, c)
}
