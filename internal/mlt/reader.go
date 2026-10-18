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

package mlt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Caps decoded memory, since run lengths and dictionaries let a small tile claim a huge one
const (
	maxTileBytes    = 50 << 20
	maxFeatureBytes = 10 << 20
)

// Approximate in-memory sizes of decoded items, for charging against the caps
const (
	stringHeaderBytes = 16
	sliceHeaderBytes  = 24
	pointBytes        = 16
	geometryBytes     = 16
	// A string header is the widest value a column holds
	maxValueBytes = stringHeaderBytes
)

// Never escapes Decode, which drops the layer instead
var errTileTooLarge = errors.New("MLT tile decodes past the size limit")

type reader struct {
	buf    []byte
	pos    int
	budget *uint64
}

func newReader(buf []byte) *reader {
	budget := uint64(maxTileBytes)
	return &reader{buf: buf, budget: &budget}
}

// Shares the parent's budget
func (r *reader) sub(buf []byte) *reader {
	return &reader{buf: buf, budget: r.budget}
}

func (r *reader) done() bool {
	return r.pos >= len(r.buf)
}

func (r *reader) remaining() int {
	return len(r.buf) - r.pos
}

// Charges count items of size bytes against the tile's budget
func (r *reader) spend(count, size uint64) error {
	if size != 0 && count > *r.budget/size {
		return errTileTooLarge
	}

	*r.budget -= count * size

	return nil
}

func (r *reader) byte() (byte, error) {
	if r.done() {
		return 0, fmt.Errorf("%w: unexpected end of data", ErrMalformed)
	}

	b := r.buf[r.pos]
	r.pos++

	return b, nil
}

func (r *reader) varint() (uint64, error) {
	v, n := binary.Uvarint(r.buf[r.pos:])
	if n <= 0 {
		return 0, fmt.Errorf("%w: invalid varint", ErrMalformed)
	}

	r.pos += n

	return v, nil
}

func (r *reader) varint32() (uint32, error) {
	v, err := r.varint()
	if err != nil {
		return 0, err
	}

	if v > math.MaxUint32 {
		return 0, fmt.Errorf("%w: varint %v exceeds 32 bits", ErrMalformed, v)
	}

	return uint32(v), nil
}

// Whether n more bytes, or items of at least a byte each, could still be read
func (r *reader) fits(n uint32) bool {
	return int64(n) <= int64(r.remaining())
}

func (r *reader) bytes(n uint32) ([]byte, error) {
	if !r.fits(n) {
		return nil, fmt.Errorf("%w: length %v exceeds the %v bytes remaining", ErrMalformed, n, r.remaining())
	}

	b := r.buf[r.pos : r.pos+int(n)]
	r.pos += int(n)

	return b, nil
}

func (r *reader) string() (string, error) {
	n, err := r.varint32()
	if err != nil {
		return "", err
	}

	b, err := r.bytes(n)

	return string(b), err
}
