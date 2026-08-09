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

package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type closable struct {
	closed bool
	err    error
}

func (c *closable) Close(_ context.Context) error {
	c.closed = true
	return c.err
}

type notClosable struct{}

func Test_CloseIfCloser(t *testing.T) {
	ctx := context.Background()

	c := &closable{}
	require.NoError(t, CloseIfCloser(ctx, c))
	assert.True(t, c.closed)

	// The majority of entities don't hold resources; those must be a silent no-op rather than an
	// error, since every call site closes indiscriminately.
	require.NoError(t, CloseIfCloser(ctx, notClosable{}))
	require.NoError(t, CloseIfCloser(ctx, nil))

	failing := &closable{err: errors.New("could not release")}
	require.Error(t, CloseIfCloser(ctx, failing))
}
