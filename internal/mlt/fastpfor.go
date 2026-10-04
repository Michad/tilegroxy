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

// MLT v1 FastPFOR uses big-endian words and 256 value blocks, per https://arxiv.org/pdf/1209.2137 section 6.
const (
	fastPFORBlockSize   = 256
	fastPFORPageSize    = 65536
	fastPFORMaxBits     = 32
	fastPFORBlockWords  = fastPFORBlockSize / 32
	vbyteContinuation   = 0x80
	vbytePayloadMask    = 0x7F
	vbyteBitsPerByte    = 7
	vbyteMaxShift       = 28
	fastPFORHeaderBytes = 2
)

type fastPFORDecoder struct {
	words []uint32
	out   []uint64
}

func decodeFastPFOR(data []byte, n uint32) ([]uint64, error) {
	if len(data)%word32Bytes != 0 {
		return nil, fmt.Errorf("%w: FastPFOR payload of %v bytes isn't whole words", ErrMalformed, len(data))
	}

	d := fastPFORDecoder{words: make([]uint32, len(data)/word32Bytes), out: make([]uint64, n)}
	for i := range d.words {
		d.words[i] = binary.BigEndian.Uint32(data[i*word32Bytes:])
	}

	pos, outPos := 0, 0

	if len(d.words) > 0 {
		aligned := int(d.words[0])
		pos = 1

		if aligned%fastPFORBlockSize != 0 || aligned > len(d.out) {
			return nil, fmt.Errorf("%w: invalid FastPFOR length %v", ErrMalformed, aligned)
		}

		for outPos < aligned {
			size := min(fastPFORPageSize, aligned-outPos)

			var err error
			if pos, err = d.page(pos, outPos, size); err != nil {
				return nil, err
			}

			outPos += size
		}
	}

	if err := d.vbyte(pos, outPos); err != nil {
		return nil, err
	}

	return d.out, nil
}

func (d *fastPFORDecoder) word(i int) (uint32, error) {
	if i < 0 || i >= len(d.words) {
		return 0, fmt.Errorf("%w: FastPFOR data truncated", ErrMalformed)
	}

	return d.words[i], nil
}

// Returns the word position where the next page starts.
func (d *fastPFORDecoder) page(pos, outPos, size int) (int, error) {
	whereMeta, err := d.word(pos)
	if err != nil {
		return 0, err
	}

	packedEnd := pos + int(whereMeta)

	byteSize, err := d.word(packedEnd)
	if err != nil || whereMeta == 0 {
		return 0, fmt.Errorf("%w: invalid FastPFOR page", ErrMalformed)
	}

	metaStart := packedEnd + 1
	bitmapPos := metaStart + (int(byteSize)+word32Bytes-1)/word32Bytes

	bitmap, err := d.word(bitmapPos)
	if err != nil {
		return 0, err
	}

	meta := make([]byte, 0, byteSize)
	for i := metaStart; i < bitmapPos; i++ {
		meta = binary.LittleEndian.AppendUint32(meta, d.words[i])
	}
	meta = meta[:byteSize]

	exceptions, next, err := d.exceptionStreams(bitmap, bitmapPos+1, size)
	if err != nil {
		return 0, err
	}

	b := fastPFORBlocks{d: d, meta: meta, exceptions: exceptions, inPos: pos + 1}
	for block := range size / fastPFORBlockSize {
		if err = b.decode(outPos + block*fastPFORBlockSize); err != nil {
			return 0, err
		}
	}

	if b.inPos != packedEnd {
		return 0, fmt.Errorf("%w: FastPFOR blocks don't fill their page", ErrMalformed)
	}

	return next, nil
}

// The bitmap flags which exception bit widths have a packed stream of values. A page can't
// patch more values than it holds.
func (d *fastPFORDecoder) exceptionStreams(bitmap uint32, pos int, pageSize int) ([][]uint32, int, error) {
	exceptions := make([][]uint32, fastPFORMaxBits+1)
	total := 0

	for width := 2; width <= fastPFORMaxBits; width++ {
		if bitmap>>(width-1)&1 == 0 {
			continue
		}

		size, err := d.word(pos)
		if err != nil {
			return nil, 0, err
		}
		pos++

		total += int(size)
		if total > pageSize {
			return nil, 0, fmt.Errorf("%w: more FastPFOR exceptions than values", ErrMalformed)
		}

		wordsNeeded := (int(size)*width + fastPFORMaxBits - 1) / fastPFORMaxBits
		if pos+wordsNeeded > len(d.words) {
			return nil, 0, fmt.Errorf("%w: FastPFOR exceptions truncated", ErrMalformed)
		}

		exceptions[width] = unpackBits(d.words[pos:pos+wordsNeeded], int(size), width)
		pos += wordsNeeded
	}

	return exceptions, pos, nil
}

type fastPFORBlocks struct {
	d          *fastPFORDecoder
	meta       []byte
	metaPos    int
	exceptions [][]uint32
	used       [fastPFORMaxBits + 1]int
	inPos      int
}

func (b *fastPFORBlocks) metaByte() (int, error) {
	if b.metaPos >= len(b.meta) {
		return 0, fmt.Errorf("%w: FastPFOR block metadata truncated", ErrMalformed)
	}

	v := b.meta[b.metaPos]
	b.metaPos++

	return int(v), nil
}

func (b *fastPFORBlocks) decode(outPos int) error {
	if b.metaPos+fastPFORHeaderBytes > len(b.meta) {
		return fmt.Errorf("%w: FastPFOR block metadata truncated", ErrMalformed)
	}

	width, exceptionCount := int(b.meta[b.metaPos]), int(b.meta[b.metaPos+1])
	b.metaPos += fastPFORHeaderBytes

	packedWords := width * fastPFORBlockWords
	if width > fastPFORMaxBits || b.inPos+packedWords > len(b.d.words) {
		return fmt.Errorf("%w: invalid FastPFOR block", ErrMalformed)
	}

	for i, v := range unpackBits(b.d.words[b.inPos:b.inPos+packedWords], fastPFORBlockSize, width) {
		b.d.out[outPos+i] = uint64(v)
	}
	b.inPos += packedWords

	if exceptionCount == 0 {
		return nil
	}

	return b.patch(outPos, width, exceptionCount)
}

// Exceptions add the high bits that didn't fit in the block's width, at the positions listed in the metadata.
func (b *fastPFORBlocks) patch(outPos, width, count int) error {
	maxBits, err := b.metaByte()
	if err != nil {
		return err
	}

	extra := maxBits - width
	if extra < 1 || maxBits > fastPFORMaxBits {
		return fmt.Errorf("%w: invalid FastPFOR exception width", ErrMalformed)
	}

	for range count {
		pos, err := b.metaByte()
		if err != nil {
			return err
		}

		high := uint32(1)
		if extra > 1 {
			stream := b.exceptions[extra]
			if b.used[extra] >= len(stream) {
				return fmt.Errorf("%w: FastPFOR exceptions exhausted", ErrMalformed)
			}

			high = stream[b.used[extra]]
			b.used[extra]++
		}

		b.d.out[outPos+pos] |= uint64(high) << width
	}

	return nil
}

// FastPFOR's variable byte coding marks the final byte of each value with the high bit.
func (d *fastPFORDecoder) vbyte(pos, outPos int) error {
	var acc uint64

	shift := 0
	for i := pos * word32Bytes; outPos < len(d.out); i++ {
		if i >= len(d.words)*word32Bytes {
			return fmt.Errorf("%w: FastPFOR tail truncated", ErrMalformed)
		}

		b := d.words[i/word32Bytes] >> (i % word32Bytes * bitsPerByte) & math.MaxUint8
		acc |= uint64(b&vbytePayloadMask) << shift

		if b&vbyteContinuation != 0 {
			d.out[outPos] = acc & math.MaxUint32
			outPos++
			acc, shift = 0, 0

			continue
		}

		shift += vbyteBitsPerByte
		if shift > vbyteMaxShift {
			return fmt.Errorf("%w: FastPFOR tail value too long", ErrMalformed)
		}
	}

	return nil
}

// Values are packed least significant bit first, spanning word boundaries.
func unpackBits(words []uint32, count, width int) []uint32 {
	out := make([]uint32, count)
	if width == 0 {
		return out
	}

	mask := uint64(1)<<width - 1

	bit := 0
	for i := range out {
		w := bit / fastPFORMaxBits
		offset := bit % fastPFORMaxBits

		v := uint64(words[w]) >> offset
		if offset+width > fastPFORMaxBits && w+1 < len(words) {
			v |= uint64(words[w+1]) << (fastPFORMaxBits - offset)
		}

		out[i] = uint32(v & mask) // #nosec G115 -- masked to at most 32 bits
		bit += width
	}

	return out
}
