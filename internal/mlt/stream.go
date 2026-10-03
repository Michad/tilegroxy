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
	"fmt"
	"math"
)

// Stream categories, the high nibble of the stream type byte.
const (
	categoryPresent uint8 = iota
	categoryData
	categoryOffset
	categoryLength
)

// Subtypes of categoryData.
const (
	dataNone uint8 = iota
	dataSingle
	dataShared
	dataVertex
	dataMorton
	dataFSST
)

// Subtypes of categoryOffset.
const (
	offsetVertex uint8 = iota
	offsetIndex
	offsetString
)

// Subtypes of categoryLength.
const (
	lengthVarBinary uint8 = iota
	lengthGeometries
	lengthParts
	lengthRings
	lengthTriangles
	lengthSymbol
	lengthDictionary
)

// Logical encodings, as the encoding byte with its physical bits cleared.
const (
	logicalNone               uint8 = 0x00
	logicalDelta              uint8 = 0x20
	logicalDeltaRle           uint8 = 0x2C
	logicalComponentwiseDelta uint8 = 0x40
	logicalRle                uint8 = 0x60
	logicalMorton             uint8 = 0x80
	logicalMortonDelta        uint8 = 0x84
	logicalMortonRle          uint8 = 0x8C
)

// Physical encodings, the low bits of the encoding byte.
const (
	physicalNone uint8 = iota
	physicalFastPFOR
	physicalVarint
)

const (
	categoryShift = 4
	subtypeMask   = 0x0F
	physicalMask  = 0x03
	logicalMask   = 0xFC
	maxMortonBits = 16
	bitsPerByte   = 8
	word32Bytes   = 4
	word64Bytes   = 8
	signShift32   = 31
	signShift64   = 63
)

type stream struct {
	category    uint8
	subtype     uint8
	logical     uint8
	physical    uint8
	numValues   uint32
	runs        uint32
	rleValues   uint32
	mortonBits  uint32
	mortonShift uint32
	data        []byte
}

func (s stream) is(category, subtype uint8) bool {
	return s.category == category && s.subtype == subtype
}

// Boolean streams never carry run counts in their header, since they follow from numValues.
func readStream(r *reader, isBool bool) (stream, error) {
	var s stream

	typ, err := r.byte()
	if err != nil {
		return s, err
	}

	enc, err := r.byte()
	if err != nil {
		return s, err
	}

	s.category, s.subtype = typ>>categoryShift, typ&subtypeMask
	s.logical, s.physical = enc&logicalMask, enc&physicalMask

	if s.category > categoryLength || s.physical > physicalVarint {
		return s, fmt.Errorf("%w: invalid stream type %#x or encoding %#x", ErrMalformed, typ, enc)
	}

	if s.numValues, err = r.varint32(); err != nil {
		return s, err
	}

	byteLength, err := r.varint32()
	if err != nil {
		return s, err
	}

	if err = s.readExtraHeader(r, isBool); err != nil {
		return s, err
	}

	s.data, err = r.bytes(byteLength)

	return s, err
}

func (s *stream) readExtraHeader(r *reader, isBool bool) error {
	var err error

	switch s.logical {
	case logicalNone, logicalDelta, logicalComponentwiseDelta:
	case logicalRle, logicalDeltaRle:
		if !isBool {
			if s.runs, err = r.varint32(); err == nil {
				s.rleValues, err = r.varint32()
			}
		}
	case logicalMorton, logicalMortonDelta, logicalMortonRle:
		if s.mortonBits, err = r.varint32(); err == nil {
			s.mortonShift, err = r.varint32()
		}

		if err == nil && s.mortonBits > maxMortonBits {
			err = fmt.Errorf("%w: morton bits %v exceeds %v", ErrMalformed, s.mortonBits, maxMortonBits)
		}
	default:
		err = fmt.Errorf("%w: invalid logical encoding %#x", ErrMalformed, s.logical)
	}

	return err
}

// Decodes the payload into unsigned words, 64 bit if wide and 32 bit otherwise.
func (s stream) words(r *reader, wide bool) ([]uint64, error) {
	if err := r.spend(uint64(s.numValues), word64Bytes); err != nil {
		return nil, err
	}

	switch s.physical {
	case physicalNone:
		return fixedWidthWords(s.data, s.numValues, wide)
	case physicalFastPFOR:
		if wide {
			return nil, fmt.Errorf("%w: FastPFOR on a 64 bit stream", ErrMalformed)
		}

		return decodeFastPFOR(s.data, s.numValues)
	default:
		return varintWords(s.data, s.numValues, wide)
	}
}

func fixedWidthWords(data []byte, n uint32, wide bool) ([]uint64, error) {
	width := word32Bytes
	if wide {
		width = word64Bytes
	}

	if uint64(len(data)) != uint64(n)*uint64(width) {
		return nil, fmt.Errorf("%w: %v bytes can't hold %v words of %v bytes", ErrMalformed, len(data), n, width)
	}

	out := make([]uint64, n)
	for i := range out {
		if wide {
			out[i] = binary.LittleEndian.Uint64(data[i*width:])
		} else {
			out[i] = uint64(binary.LittleEndian.Uint32(data[i*width:]))
		}
	}

	return out, nil
}

func varintWords(data []byte, n uint32, wide bool) ([]uint64, error) {
	if uint64(n) > uint64(len(data)) {
		return nil, fmt.Errorf("%w: %v bytes can't hold %v varints", ErrMalformed, len(data), n)
	}

	out := make([]uint64, n)
	pos := 0

	for i := range out {
		v, read := binary.Uvarint(data[pos:])
		if read <= 0 || (!wide && v > math.MaxUint32) {
			return nil, fmt.Errorf("%w: invalid varint in stream", ErrMalformed)
		}

		out[i] = v
		pos += read
	}

	return out, nil
}

// Decodes integer values as unsigned, truncated to 32 bits unless wide.
func (s stream) unsigned(r *reader, wide bool) ([]uint64, error) {
	w, err := s.expanded(r, wide)
	if err != nil {
		return nil, err
	}

	if s.logical == logicalDelta || s.logical == logicalDeltaRle {
		return deltaDecode(w, wide), nil
	}

	return w, nil
}

// Decodes integer values of a signed type.
func (s stream) signed(r *reader, wide bool) ([]int64, error) {
	w, err := s.expanded(r, wide)
	if err != nil {
		return nil, err
	}

	out := make([]int64, len(w))

	switch s.logical {
	case logicalDelta, logicalDeltaRle:
		for i, v := range deltaDecode(w, wide) {
			out[i] = asSigned(v, wide)
		}
	default:
		for i, v := range w {
			out[i] = unzigzag(v, wide)
		}
	}

	return out, nil
}

// Physically decodes and expands any run length encoding, leaving delta and zigzag coding in place.
func (s stream) expanded(r *reader, wide bool) ([]uint64, error) {
	switch s.logical {
	case logicalNone, logicalDelta:
		return s.words(r, wide)
	case logicalRle, logicalDeltaRle:
		w, err := s.words(r, wide)
		if err != nil {
			return nil, err
		}

		return expandRuns(r, w, s.runs, s.rleValues)
	default:
		return nil, fmt.Errorf("%w: logical encoding %#x on an integer stream", ErrMalformed, s.logical)
	}
}

// Run lengths come first, then the value of each run.
func expandRuns(r *reader, w []uint64, runs uint32, total uint32) ([]uint64, error) {
	if uint64(len(w)) != 2*uint64(runs) {
		return nil, fmt.Errorf("%w: %v words can't hold %v runs", ErrMalformed, len(w), runs)
	}

	var sum uint64
	for _, n := range w[:runs] {
		if n > uint64(total)-sum {
			return nil, fmt.Errorf("%w: runs exceed %v values", ErrMalformed, total)
		}

		sum += n
	}

	if sum != uint64(total) {
		return nil, fmt.Errorf("%w: runs don't sum to %v values", ErrMalformed, total)
	}

	if err := r.spend(sum, word64Bytes); err != nil {
		return nil, err
	}

	out := make([]uint64, 0, sum)
	for i, n := range w[:runs] {
		for range n {
			out = append(out, w[int(runs)+i])
		}
	}

	return out, nil
}

// Deltas are zigzag coded and accumulate with wrapping at the word width.
func deltaDecode(w []uint64, wide bool) []uint64 {
	out := make([]uint64, len(w))

	var acc uint64
	for i, v := range w {
		acc += uint64(unzigzag(v, wide)) // #nosec G115 -- two's complement addition wraps as intended
		if !wide {
			acc &= math.MaxUint32
		}

		out[i] = acc
	}

	return out
}

func unzigzag(v uint64, wide bool) int64 {
	if wide {
		return int64(v>>1) ^ -int64(v&1) // #nosec G115 -- v>>1 always fits in int64
	}

	u := uint32(v)                          // #nosec G115 -- 32 bit streams are validated to hold 32 bit words
	return int64(int32(u>>1) ^ -int32(u&1)) // #nosec G115 -- u>>1 always fits in int32
}

func zigzag(v int64, wide bool) uint64 {
	if wide {
		return uint64((v << 1) ^ (v >> signShift64)) // #nosec G115 -- reinterprets the zigzag bit pattern
	}

	w := int32(v)                                        // #nosec G115 -- 32 bit values are truncated by design
	return uint64(uint32((w << 1) ^ (w >> signShift32))) // #nosec G115 -- reinterprets the zigzag bit pattern
}

// Reinterprets a word's bits as a two's complement value of the stream's width.
func asSigned(v uint64, wide bool) int64 {
	if wide {
		return int64(v) // #nosec G115 -- reinterprets the bit pattern
	}

	return int64(int32(uint32(v))) // #nosec G115 -- reinterprets the bit pattern of a 32 bit word
}

func (s stream) bools(r *reader) ([]bool, error) {
	n := s.numValues
	nBytes := (uint64(n) + bitsPerByte - 1) / bitsPerByte

	if err := r.spend(uint64(n), 1); err != nil {
		return nil, err
	}

	var bitmap []byte

	switch {
	case s.physical == physicalNone && s.logical == logicalRle:
		var err error
		if bitmap, err = decodeByteRle(s.data, nBytes); err != nil {
			return nil, err
		}
	case s.physical == physicalNone && s.logical == logicalNone && uint64(len(s.data)) == nBytes:
		bitmap = s.data
	default:
		return nil, fmt.Errorf("%w: unsupported boolean stream encoding %#x", ErrMalformed, s.logical|s.physical)
	}

	out := make([]bool, n)
	for i := range out {
		out[i] = bitmap[i/bitsPerByte]>>(i%bitsPerByte)&1 == 1
	}

	return out, nil
}

// ORC byte RLE: a control byte below 128 repeats the next byte control+3 times, else 256-control literal bytes follow.
func decodeByteRle(data []byte, n uint64) ([]byte, error) {
	out := make([]byte, 0, n)
	pos := 0

	for uint64(len(out)) < n && pos < len(data) {
		c := int(data[pos])
		pos++

		if c < byteRleLiteralMin {
			if pos >= len(data) {
				break
			}

			for range c + byteRleMinRepeat {
				out = append(out, data[pos])
			}
			pos++

			continue
		}

		count := byteRleControlBase - c
		if pos+count > len(data) {
			break
		}

		out = append(out, data[pos:pos+count]...)
		pos += count
	}

	if uint64(len(out)) != n {
		return nil, fmt.Errorf("%w: boolean runs expand to %v bytes rather than %v", ErrMalformed, len(out), n)
	}

	return out, nil
}

const (
	byteRleLiteralMin  = 128
	byteRleMinRepeat   = 3
	byteRleMaxRepeat   = 130
	byteRleMaxLiteral  = 128
	byteRleControlBase = 256
)

func (s stream) floats(r *reader, wide bool) ([]float64, error) {
	if s.logical != logicalNone || s.physical != physicalNone {
		return nil, fmt.Errorf("%w: floats must be stored plain", ErrMalformed)
	}

	w, err := s.words(r, wide)
	if err != nil {
		return nil, err
	}

	out := make([]float64, len(w))
	for i, v := range w {
		if wide {
			out[i] = math.Float64frombits(v)
		} else {
			out[i] = float64(math.Float32frombits(uint32(v))) // #nosec G115 -- 32 bit words
		}
	}

	return out, nil
}

// Decodes a vertex stream into interleaved x, y coordinates.
func (s stream) vertices(r *reader) ([]int32, error) {
	switch s.logical {
	case logicalNone, logicalDelta:
		v, err := s.signed(r, false)
		return narrow(v), err
	case logicalComponentwiseDelta:
		return s.componentwiseDelta(r)
	case logicalMorton, logicalMortonDelta:
		return s.morton(r)
	default:
		return nil, fmt.Errorf("%w: logical encoding %#x on a vertex stream", ErrMalformed, s.logical)
	}
}

func narrow(v []int64) []int32 {
	out := make([]int32, len(v))
	for i, x := range v {
		out[i] = int32(x) // #nosec G115 -- values come from 32 bit streams
	}

	return out
}

func (s stream) componentwiseDelta(r *reader) ([]int32, error) {
	w, err := s.words(r, false)
	if err != nil {
		return nil, err
	}

	if len(w)%2 != 0 {
		return nil, fmt.Errorf("%w: odd number of vertex coordinates", ErrMalformed)
	}

	out := make([]int32, len(w))

	var x, y int32
	for i := 0; i < len(w); i += 2 {
		x += int32(unzigzag(w[i], false))   // #nosec G115 -- 32 bit zigzag values
		y += int32(unzigzag(w[i+1], false)) // #nosec G115 -- 32 bit zigzag values
		out[i], out[i+1] = x, y
	}

	return out, nil
}

// Each code interleaves the bits of one shifted vertex. MortonDelta stores plain differences between codes.
func (s stream) morton(r *reader) ([]int32, error) {
	w, err := s.words(r, false)
	if err != nil {
		return nil, err
	}

	out := make([]int32, 2*len(w))

	var code uint32
	for i, v := range w {
		if s.logical == logicalMortonDelta {
			code += uint32(v) // #nosec G115 -- 32 bit words
		} else {
			code = uint32(v) // #nosec G115 -- 32 bit words
		}

		var x, y uint32
		for bit := range s.mortonBits {
			x |= (code >> (2 * bit) & 1) << bit
			y |= (code >> (2*bit + 1) & 1) << bit
		}

		out[2*i] = int32(x - s.mortonShift)   // #nosec G115 -- wraps like the reference decoder
		out[2*i+1] = int32(y - s.mortonShift) // #nosec G115 -- wraps like the reference decoder
	}

	return out, nil
}
