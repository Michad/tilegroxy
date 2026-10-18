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
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
)

type Compression uint8

const (
	CompressionUnknown Compression = 0
	CompressionNone    Compression = 1
	CompressionGzip    Compression = 2
	CompressionBrotli  Compression = 3
	CompressionZstd    Compression = 4
)

var errUnsupportedCompression = errors.New("pmtiles: unsupported compression, only none and gzip are supported")

func (c Compression) supported() bool {
	return c == CompressionNone || c == CompressionGzip
}

func decompress(data []byte, c Compression, maxLength uint64) ([]byte, error) {
	switch c {
	case CompressionNone:
		return data, nil
	case CompressionGzip:
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer r.Close()

		// Clamp so int64(limit)+1 can't wrap when maxLength is near uint64 max
		limit := min(maxLength, math.MaxInt64-1)

		// One byte past the limit detects oversized output without buffering all of it
		out, err := io.ReadAll(io.LimitReader(r, int64(limit)+1)) // #nosec G115 -- limit is clamped below MaxInt64
		if err != nil {
			return nil, err
		}
		if uint64(len(out)) > maxLength {
			return nil, fmt.Errorf("pmtiles: decompressed data exceeds %d bytes", maxLength)
		}

		return out, nil
	case CompressionUnknown, CompressionBrotli, CompressionZstd:
	}

	return nil, errUnsupportedCompression
}
