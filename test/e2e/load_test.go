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
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_Load_AllRequestsSucceedAgainstHealthyServer(t *testing.T) {
	inst := Start(t, Config{Raw: staticLayerConfig})

	load := inst.StartLoad("/tiles/color/8/12/32", 4)
	time.Sleep(2 * time.Second) // Deliberately generating load for a window, not waiting on a condition.
	res := load.Stop()

	assert.Positive(t, res.Total)
	assert.Equal(t, res.Total, res.ByStatus[http.StatusOK])
	assert.Zero(t, res.TransportErrors)
	assert.True(t, res.AllOK())
}
