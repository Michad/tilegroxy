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

package entities

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Entities_CloseNil(t *testing.T) {
	var e *Entities

	require.NoError(t, e.Close(context.Background()))
}

// Entities is exported and constructed directly, so a generation missing any given entity has to close
// without panicking on the nil field.
func Test_Entities_CloseWithUnsetEntities(t *testing.T) {
	e := &Entities{}

	assert.NotPanics(t, func() {
		require.NoError(t, e.Close(context.Background()))
	})
}

func Test_Entities_CloseIsIdempotent(t *testing.T) {
	e := &Entities{}

	require.NoError(t, e.Close(context.Background()))
	require.NoError(t, e.Close(context.Background()))
}
