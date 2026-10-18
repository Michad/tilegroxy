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

package pmtiles

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Decompress_None(t *testing.T) {
	got, err := decompress([]byte("abc"), CompressionNone, 10)

	require.NoError(t, err)
	assert.Equal(t, []byte("abc"), got)
}

func Test_Decompress_Gzip(t *testing.T) {
	got, err := decompress(gzipBytes(t, []byte("hello")), CompressionGzip, 10)

	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), got)
}

func Test_Decompress_GzipOverLimit(t *testing.T) {
	_, err := decompress(gzipBytes(t, make([]byte, 100)), CompressionGzip, 50)

	require.Error(t, err)
}

func Test_Decompress_InvalidGzip(t *testing.T) {
	_, err := decompress([]byte("not gzip"), CompressionGzip, 50)

	require.Error(t, err)
}

func Test_Decompress_Unsupported(t *testing.T) {
	for _, c := range []Compression{CompressionUnknown, CompressionBrotli, CompressionZstd} {
		_, err := decompress([]byte("x"), c, 50)
		require.ErrorIs(t, err, errUnsupportedCompression, "compression %d", c)
	}
}

func Test_Decompress_MaxLengthNearUint64Max_NoOverflowPanic(t *testing.T) {
	// maxLength beyond MaxInt64 must not wrap the read limit
	got, err := decompress(gzipBytes(t, []byte("hello")), CompressionGzip, math.MaxUint64)

	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), got)
}
