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

// Encode writes layers as a v1 tile. Only the simplest encodings are used, favoring
// compatibility with every decoder over size.
func Encode(layers []Layer) ([]byte, error) {
	out := []byte{}

	for _, l := range layers {
		body, err := encodeLayer(l)
		if err != nil {
			return nil, err
		}

		out = appendVarint(out, uint64(len(body))+1)
		out = append(out, tagV1)
		out = append(out, body...)
	}

	return out, nil
}

func encodeLayer(l Layer) ([]byte, error) {
	if l.Name == "" || l.Extent == 0 {
		return nil, fmt.Errorf("%w: layer needs a name and extent", ErrMalformed)
	}

	if err := l.validate(); err != nil {
		return nil, err
	}

	buf := appendString(nil, l.Name)
	buf = appendVarint(buf, uint64(l.Extent))

	// The id and geometry come first since some decoders need the feature count before the properties.
	count := uint64(1 + len(l.Columns))
	if l.ID != nil {
		count++
	}

	buf = appendVarint(buf, count)

	if l.ID != nil {
		code := codeID
		if l.ID.Long {
			code = codeLongID
		}

		buf = append(buf, withNullable(code, l.ID.Nullable))
	}

	buf = append(buf, codeGeometry)

	for _, c := range l.Columns {
		buf = appendColumnMeta(buf, c)
	}

	if l.ID != nil {
		buf = appendPresent(buf, l.ID.Nullable, l.ID.Present)
		buf = appendVarintStream(buf, categoryData, dataNone, presentOnly(l.ID.Present, l.ID.Values))
	}

	buf, err := appendGeometryColumn(buf, l.Geometries)
	if err != nil {
		return nil, err
	}

	for _, c := range l.Columns {
		if buf, err = appendColumn(buf, c); err != nil {
			return nil, err
		}
	}

	return buf, nil
}

func withNullable(code uint8, nullable bool) uint8 {
	if nullable {
		return code | nullableFlag
	}

	return code
}

func appendColumnMeta(buf []byte, c Column) []byte {
	buf = append(buf, withNullable(uint8(c.Type), c.Nullable))
	buf = appendString(buf, c.Name)

	if c.Type != ColumnSharedDict {
		return buf
	}

	buf = appendVarint(buf, uint64(len(c.Children)))
	for _, child := range c.Children {
		buf = append(buf, withNullable(uint8(ColumnString), child.Nullable))
		buf = appendString(buf, child.Name)
	}

	return buf
}

func appendColumn(buf []byte, c Column) ([]byte, error) {
	switch v := c.Values.(type) {
	case []bool:
		buf = appendPresent(buf, c.Nullable, c.Present)
		return appendBoolStream(buf, categoryData, dataNone, presentOnly(c.Present, v)), nil
	case []int32:
		return appendIntColumn(buf, c, v, func(x int32) uint64 { return zigzag(int64(x), false) }), nil
	case []int64:
		return appendIntColumn(buf, c, v, func(x int64) uint64 { return zigzag(x, true) }), nil
	case []uint32:
		return appendIntColumn(buf, c, v, func(x uint32) uint64 { return uint64(x) }), nil
	case []uint64:
		return appendIntColumn(buf, c, v, func(x uint64) uint64 { return x }), nil
	case []float32:
		return appendFloatColumn(buf, c, v, func(b []byte, x float32) []byte {
			return binary.LittleEndian.AppendUint32(b, math.Float32bits(x))
		}), nil
	case []float64:
		return appendFloatColumn(buf, c, v, func(b []byte, x float64) []byte {
			return binary.LittleEndian.AppendUint64(b, math.Float64bits(x))
		}), nil
	case []string:
		return appendStringColumn(buf, c, v), nil
	}

	if c.Type == ColumnSharedDict {
		return appendSharedDictColumn(buf, c), nil
	}

	return nil, fmt.Errorf("%w: column %v has values of type %T", ErrUnsupported, c.Name, c.Values)
}

func appendIntColumn[T any](buf []byte, c Column, values []T, word func(T) uint64) []byte {
	buf = appendPresent(buf, c.Nullable, c.Present)

	present := presentOnly(c.Present, values)
	words := make([]uint64, len(present))

	for i, v := range present {
		words[i] = word(v)
	}

	return appendVarintStream(buf, categoryData, dataNone, words)
}

func appendFloatColumn[T any](buf []byte, c Column, values []T, appendValue func([]byte, T) []byte) []byte {
	buf = appendPresent(buf, c.Nullable, c.Present)

	present := presentOnly(c.Present, values)

	var payload []byte
	for _, v := range present {
		payload = appendValue(payload, v)
	}

	return appendRawStream(buf, categoryData, dataNone, uint64(len(present)), payload)
}

// Written in the plain layout: a length per value, then the bytes of every value.
func appendStringColumn(buf []byte, c Column, values []string) []byte {
	streams := uint64(stringPlain)
	if c.Nullable {
		streams++
	}

	buf = appendVarint(buf, streams)
	buf = appendPresent(buf, c.Nullable, c.Present)

	present := presentOnly(c.Present, values)

	return appendPlainStrings(buf, lengthVarBinary, dataNone, present)
}

func appendPlainStrings(buf []byte, lengthSubtype, dataSubtype uint8, values []string) []byte {
	lengths := make([]uint64, len(values))

	var data []byte
	for i, v := range values {
		lengths[i] = uint64(len(v))
		data = append(data, v...)
	}

	buf = appendVarintStream(buf, categoryLength, lengthSubtype, lengths)

	return appendRawStream(buf, categoryData, dataSubtype, uint64(len(values)), data)
}

func appendSharedDictColumn(buf []byte, c Column) []byte {
	var dict []string
	index := map[string]uint64{}
	codes := make([][]uint64, len(c.Children))
	streams := uint64(stringPlain)

	for i, child := range c.Children {
		values, _ := child.Values.([]string)
		for _, v := range presentOnly(child.Present, values) {
			code, ok := index[v]
			if !ok {
				code = uint64(len(dict))
				index[v] = code
				dict = append(dict, v)
			}

			codes[i] = append(codes[i], code)
		}

		streams++
		if child.Nullable {
			streams++
		}
	}

	buf = appendVarint(buf, streams)
	buf = appendPlainStrings(buf, lengthDictionary, dataShared, dict)

	for i, child := range c.Children {
		childStreams := uint64(1)
		if child.Nullable {
			childStreams++
		}

		buf = appendVarint(buf, childStreams)
		buf = appendPresent(buf, child.Nullable, child.Present)
		buf = appendVarintStream(buf, categoryOffset, offsetString, codes[i])
	}

	return buf
}

func presentOnly[T any](present []bool, values []T) []T {
	if present == nil {
		return values
	}

	out := make([]T, 0, len(values))
	for i, p := range present {
		if p {
			out = append(out, values[i])
		}
	}

	return out
}

func appendPresent(buf []byte, nullable bool, present []bool) []byte {
	if !nullable {
		return buf
	}

	return appendBoolStream(buf, categoryPresent, 0, present)
}

func streamType(category, subtype uint8) byte {
	return category<<categoryShift | subtype
}

func appendVarint(buf []byte, v uint64) []byte {
	return binary.AppendUvarint(buf, v)
}

func appendString(buf []byte, s string) []byte {
	buf = appendVarint(buf, uint64(len(s)))
	return append(buf, s...)
}

func appendStreamHeader(buf []byte, category, subtype, encoding uint8, numValues, byteLength uint64) []byte {
	buf = append(buf, streamType(category, subtype), encoding)
	buf = appendVarint(buf, numValues)

	return appendVarint(buf, byteLength)
}

func appendVarintStream(buf []byte, category, subtype uint8, values []uint64) []byte {
	var payload []byte
	for _, v := range values {
		payload = appendVarint(payload, v)
	}

	buf = appendStreamHeader(buf, category, subtype, logicalNone|physicalVarint, uint64(len(values)), uint64(len(payload)))

	return append(buf, payload...)
}

func appendRawStream(buf []byte, category, subtype uint8, numValues uint64, payload []byte) []byte {
	// Raw bytes have no physical encoding, whose code is zero.
	buf = appendStreamHeader(buf, category, subtype, logicalNone, numValues, uint64(len(payload)))
	return append(buf, payload...)
}

// Booleans are always run length encoded, since some decoders don't accept a plain bitmap for presence.
func appendBoolStream(buf []byte, category, subtype uint8, values []bool) []byte {
	bitmap := make([]byte, (len(values)+bitsPerByte-1)/bitsPerByte)
	for i, v := range values {
		if v {
			bitmap[i/bitsPerByte] |= 1 << (i % bitsPerByte)
		}
	}

	payload := encodeByteRle(bitmap)
	buf = appendStreamHeader(buf, category, subtype, logicalRle, uint64(len(values)), uint64(len(payload)))

	return append(buf, payload...)
}

func encodeByteRle(src []byte) []byte {
	var out []byte

	for i := 0; i < len(src); {
		run := 1
		for i+run < len(src) && src[i+run] == src[i] && run < byteRleMaxRepeat {
			run++
		}

		if run >= byteRleMinRepeat {
			out = append(out, byte(run-byteRleMinRepeat), src[i])
			i += run

			continue
		}

		start := i
		for i < len(src) && i-start < byteRleMaxLiteral && !repeatsAt(src, i) {
			i++
		}

		out = append(out, byte(byteRleControlBase-(i-start))) // #nosec G115 -- literal runs are 1 to 128 bytes long
		out = append(out, src[start:i]...)
	}

	return out
}

func repeatsAt(src []byte, i int) bool {
	return i+byteRleMinRepeat <= len(src) && src[i] == src[i+1] && src[i] == src[i+2]
}

// Vertices are written as zigzag coded deltas from the previous vertex, per axis.
func appendVertexStream(buf []byte, vertices []int32) []byte {
	var payload []byte

	var prevX, prevY int32
	for i := 0; i < len(vertices); i += 2 {
		x, y := vertices[i], vertices[i+1]
		payload = appendVarint(payload, zigzag(int64(x-prevX), false))
		payload = appendVarint(payload, zigzag(int64(y-prevY), false))
		prevX, prevY = x, y
	}

	buf = appendStreamHeader(buf, categoryData, dataVertex, logicalComponentwiseDelta|physicalVarint, uint64(len(vertices)), uint64(len(payload)))

	return append(buf, payload...)
}
