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

package static

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPackage(t *testing.T) {
	assert.Equal(t, "github.com/michad/tilegroxy", GetPackage())
}

func TestGetVersionInformation_DefaultsWhenUnset(t *testing.T) {
	version, ref, date := GetVersionInformation()

	assert.Equal(t, "v0.X.Y", version)
	assert.Equal(t, "HEAD", ref)
	assert.Equal(t, "Unknown", date)
}

func TestGetVersionInformation_UsesLinkerVars(t *testing.T) {
	tilegroxyVersion = "v1.2.3"
	tilegroxyBuildRef = "abc123"
	tilegroxyBuildDate = "2026-01-01"
	defer func() {
		tilegroxyVersion = ""
		tilegroxyBuildRef = ""
		tilegroxyBuildDate = ""
	}()

	version, ref, date := GetVersionInformation()

	assert.Equal(t, "v1.2.3", version)
	assert.Equal(t, "abc123", ref)
	assert.Equal(t, "2026-01-01", date)
}
