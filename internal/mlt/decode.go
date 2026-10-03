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
	"errors"
	"fmt"
	"math"
	"slices"
)

const tagV1 = 1

// Column type codes that aren't property columns. The low bit of every code marks it nullable.
const (
	codeID       uint8 = 0
	codeLongID   uint8 = 2
	codeGeometry uint8 = 4
	nullableFlag uint8 = 1
)

// The number of streams in each string column layout, not counting a present stream.
const (
	stringPlain = 2 + iota
	stringDictionary
	stringFSST
	stringFSSTDictionary
)

type columnMeta struct {
	code     uint8
	name     string
	children []columnMeta
}

func (m columnMeta) base() uint8 {
	return m.code &^ nullableFlag
}

func (m columnMeta) nullable() bool {
	return m.code&nullableFlag != 0
}

// Decode parses every v1 layer of a tile. Layers in any other format, or that would take the
// tile past maxTileBytes, are skipped and counted. Features past maxFeatureBytes are dropped.
func Decode(data []byte) ([]Layer, int, error) {
	r := newReader(data)
	layers := []Layer{}
	skipped := 0

	for !r.done() {
		size, err := r.varint32()
		if err != nil {
			return nil, 0, err
		}

		if size == 0 {
			return nil, 0, fmt.Errorf("%w: layer of zero bytes", ErrMalformed)
		}

		tag, err := r.byte()
		if err != nil {
			return nil, 0, err
		}

		body, err := r.bytes(size - 1)
		if err != nil {
			return nil, 0, err
		}

		if tag != tagV1 {
			skipped++
			continue
		}

		l, err := decodeLayer(r.sub(body))
		if errors.Is(err, errTileTooLarge) {
			skipped++
			continue
		}

		if err != nil {
			return nil, 0, err
		}

		layers = append(layers, l)
	}

	return layers, skipped, nil
}

func decodeLayer(r *reader) (Layer, error) {
	var l Layer
	var err error

	if l.Name, err = r.string(); err != nil {
		return l, err
	}

	if l.Extent, err = r.varint32(); err != nil {
		return l, err
	}

	if l.Name == "" || l.Extent == 0 {
		return l, fmt.Errorf("%w: layer needs a name and extent", ErrMalformed)
	}

	metas, err := readColumnMetas(r)
	if err != nil {
		return l, err
	}

	var geometrySizes []int

	for _, m := range metas {
		switch m.base() {
		case codeID, codeLongID:
			l.ID, err = decodeIDColumn(r, m)
		case codeGeometry:
			l.Geometries, geometrySizes, err = decodeGeometryColumn(r)
		default:
			var c Column
			c, err = decodePropertyColumn(r, m)
			l.Columns = append(l.Columns, c)
		}

		if err != nil {
			return l, err
		}
	}

	if !r.done() {
		return l, fmt.Errorf("%w: %v unread bytes after the last column", ErrMalformed, r.remaining())
	}

	if geometrySizes == nil {
		return l, fmt.Errorf("%w: layer %v has no geometry column", ErrMalformed, l.Name)
	}

	if err = l.validate(); err != nil {
		return l, err
	}

	return l.dropOversized(geometrySizes), nil
}

// Oversized geometries were never built, so they're left nil.
func (l Layer) dropOversized(geometrySizes []int) Layer {
	keep := make([]int, 0, len(l.Geometries))

	for i, g := range l.Geometries {
		if g != nil && geometrySizes[i]+l.propertyBytes(i) <= maxFeatureBytes {
			keep = append(keep, i)
		}
	}

	if len(keep) == len(l.Geometries) {
		return l
	}

	return l.Select(keep)
}

func (l Layer) propertyBytes(feature int) int {
	n := 0
	if l.ID != nil {
		n += word64Bytes
	}

	for _, c := range l.Columns {
		n += c.rowBytes(feature)
	}

	return n
}

func (c Column) rowBytes(feature int) int {
	n := 0

	switch v := c.Values.(type) {
	case nil:
		for _, child := range c.Children {
			n += child.rowBytes(feature)
		}
	case []string:
		n = stringHeaderBytes + len(v[feature])
	default:
		n = word64Bytes
	}

	return n
}

func readColumnMetas(r *reader) ([]columnMeta, error) {
	count, err := r.varint32()
	if err != nil {
		return nil, err
	}

	if !r.fits(count) {
		return nil, fmt.Errorf("%w: %v columns can't fit in %v bytes", ErrMalformed, count, r.remaining())
	}

	metas := make([]columnMeta, count)
	ids, geometries := 0, 0

	for i := range metas {
		if metas[i], err = readColumnMeta(r); err != nil {
			return nil, err
		}

		switch metas[i].base() {
		case codeID, codeLongID:
			ids++
		case codeGeometry:
			geometries++
		}
	}

	if ids > 1 || geometries > 1 {
		return nil, fmt.Errorf("%w: more than one id or geometry column", ErrMalformed)
	}

	return metas, nil
}

func readColumnMeta(r *reader) (columnMeta, error) {
	var m columnMeta
	var err error

	if m.code, err = r.byte(); err != nil {
		return m, err
	}

	switch base := m.base(); {
	case base == codeID || base == codeLongID:
		return m, nil
	case m.code == codeGeometry:
		return m, nil
	case base == uint8(ColumnBool) || (base >= uint8(ColumnInt32) && base <= uint8(ColumnString)) || m.code == uint8(ColumnSharedDict):
	default:
		return m, fmt.Errorf("%w: unknown column type %v", ErrMalformed, m.code)
	}

	if m.name, err = r.string(); err != nil || m.code != uint8(ColumnSharedDict) {
		return m, err
	}

	count, err := r.varint32()
	if err != nil {
		return m, err
	}

	if !r.fits(count) {
		return m, fmt.Errorf("%w: %v children can't fit in %v bytes", ErrMalformed, count, r.remaining())
	}

	m.children = make([]columnMeta, count)
	for i := range m.children {
		child := &m.children[i]
		if child.code, err = r.byte(); err != nil {
			return m, err
		}

		if child.base() != uint8(ColumnString) {
			return m, fmt.Errorf("%w: shared dictionary child of type %v", ErrMalformed, child.code)
		}

		if child.name, err = r.string(); err != nil {
			return m, err
		}
	}

	return m, nil
}

func readPresent(r *reader, nullable bool) ([]bool, error) {
	if !nullable {
		return nil, nil
	}

	s, err := readStream(r, true)
	if err != nil {
		return nil, err
	}

	return s.bools(r)
}

// Streams hold only present values, so they're spread out to one per feature with gaps left zero.
func expand[T any](r *reader, present []bool, values []T) ([]T, error) {
	if present == nil {
		return values, nil
	}

	if err := r.spend(uint64(len(present)), maxValueBytes); err != nil {
		return nil, err
	}

	out := make([]T, len(present))
	i := 0

	for f, p := range present {
		if !p {
			continue
		}

		if i >= len(values) {
			return nil, fmt.Errorf("%w: fewer values than present features", ErrMalformed)
		}

		out[f] = values[i]
		i++
	}

	if i != len(values) {
		return nil, fmt.Errorf("%w: more values than present features", ErrMalformed)
	}

	return out, nil
}

func decodeIDColumn(r *reader, m columnMeta) (*IDColumn, error) {
	id := &IDColumn{Long: m.base() == codeLongID, Nullable: m.nullable()}

	var err error
	if id.Present, err = readPresent(r, id.Nullable); err != nil {
		return nil, err
	}

	s, err := readStream(r, false)
	if err != nil {
		return nil, err
	}

	values, err := s.unsigned(r, id.Long)
	if err != nil {
		return nil, err
	}

	id.Values, err = expand(r, id.Present, values)

	return id, err
}

func decodePropertyColumn(r *reader, m columnMeta) (Column, error) {
	c := Column{Type: ColumnType(m.base()), Name: m.name, Nullable: m.nullable()}

	switch c.Type {
	case ColumnString:
		return c, decodeStringColumn(r, &c)
	case ColumnSharedDict:
		return c, decodeSharedDictColumn(r, &c, m.children)
	case ColumnBool, ColumnInt32, ColumnUint32, ColumnInt64, ColumnUint64, ColumnFloat, ColumnDouble:
	}

	return c, decodeScalarColumn(r, &c)
}

func decodeScalarColumn(r *reader, c *Column) error {
	var err error
	if c.Present, err = readPresent(r, c.Nullable); err != nil {
		return err
	}

	s, err := readStream(r, c.Type == ColumnBool)
	if err != nil {
		return err
	}

	switch c.Type {
	case ColumnBool:
		c.Values, err = decodeAndExpand(c.Present, s.bools, func(v bool) bool { return v }, r)
	case ColumnInt32:
		c.Values, err = decodeAndExpand(c.Present, signedDecoder(s, false), func(v int64) int32 { return int32(v) }, r) // #nosec G115 -- 32 bit stream
	case ColumnInt64:
		c.Values, err = decodeAndExpand(c.Present, signedDecoder(s, true), func(v int64) int64 { return v }, r)
	case ColumnUint32:
		c.Values, err = decodeAndExpand(c.Present, unsignedDecoder(s, false), func(v uint64) uint32 { return uint32(v) }, r) // #nosec G115 -- 32 bit stream
	case ColumnUint64:
		c.Values, err = decodeAndExpand(c.Present, unsignedDecoder(s, true), func(v uint64) uint64 { return v }, r)
	case ColumnFloat:
		c.Values, err = decodeAndExpand(c.Present, floatDecoder(s, false), func(v float64) float32 { return float32(v) }, r)
	case ColumnDouble:
		c.Values, err = decodeAndExpand(c.Present, floatDecoder(s, true), func(v float64) float64 { return v }, r)
	case ColumnString, ColumnSharedDict:
		err = fmt.Errorf("%w: %v isn't a scalar column", ErrMalformed, c.Type)
	}

	return err
}

func signedDecoder(s stream, wide bool) func(*reader) ([]int64, error) {
	return func(r *reader) ([]int64, error) { return s.signed(r, wide) }
}

func unsignedDecoder(s stream, wide bool) func(*reader) ([]uint64, error) {
	return func(r *reader) ([]uint64, error) { return s.unsigned(r, wide) }
}

func floatDecoder(s stream, wide bool) func(*reader) ([]float64, error) {
	return func(r *reader) ([]float64, error) { return s.floats(r, wide) }
}

func decodeAndExpand[S, T any](present []bool, decode func(*reader) ([]S, error), convert func(S) T, r *reader) ([]T, error) {
	raw, err := decode(r)
	if err != nil {
		return nil, err
	}

	values := make([]T, len(raw))
	for i, v := range raw {
		values[i] = convert(v)
	}

	return expand(r, present, values)
}

func decodeStringColumn(r *reader, c *Column) error {
	count, err := r.varint32()
	if err != nil {
		return err
	}

	if c.Present, err = readPresent(r, c.Nullable); err != nil {
		return err
	}

	if c.Nullable {
		if count == 0 {
			return fmt.Errorf("%w: string column %v has no streams", ErrMalformed, c.Name)
		}
		count--
	}

	if count < stringPlain || count > stringFSSTDictionary {
		return fmt.Errorf("%w: string column %v has %v streams", ErrMalformed, c.Name, count)
	}

	streams := make([]stream, count)
	for i := range streams {
		if streams[i], err = readStream(r, false); err != nil {
			return err
		}
	}

	values, err := decodeStrings(r, streams)
	if err != nil {
		return err
	}

	c.Values, err = expand(r, c.Present, values)

	return err
}

// The layouts are told apart by stream count: plain, dictionary, FSST, then FSST dictionary.
func decodeStrings(r *reader, s []stream) ([]string, error) {
	switch len(s) {
	case stringPlain:
		return plainStrings(r, s[0], s[1])
	case stringDictionary:
		dict, err := plainStrings(r, s[0], s[2])
		if err != nil {
			return nil, err
		}

		return lookupDictionary(r, s[1], dict)
	case stringFSST:
		return fsstStrings(r, s[0], s[1], s[2], s[3])
	default:
		dict, err := fsstStrings(r, s[0], s[1], s[2], s[3])
		if err != nil {
			return nil, err
		}

		return lookupDictionary(r, s[4], dict)
	}
}

func plainStrings(r *reader, lengths stream, data stream) ([]string, error) {
	n, err := lengths.unsigned(r, false)
	if err != nil {
		return nil, err
	}

	if err = r.spend(uint64(len(data.data)), 1); err != nil {
		return nil, err
	}

	return splitStrings(data.data, n)
}

func splitStrings(data []byte, lengths []uint64) ([]string, error) {
	out := make([]string, len(lengths))
	pos := uint64(0)

	for i, n := range lengths {
		if n > uint64(len(data))-pos {
			return nil, fmt.Errorf("%w: string lengths overrun their data", ErrMalformed)
		}

		out[i] = string(data[pos : pos+n])
		pos += n
	}

	if pos != uint64(len(data)) {
		return nil, fmt.Errorf("%w: string data longer than its lengths", ErrMalformed)
	}

	return out, nil
}

// Entries share memory, but each use is charged since Encode may write every value out separately.
func lookupDictionary(r *reader, codes stream, dict []string) ([]string, error) {
	c, err := codes.unsigned(r, false)
	if err != nil {
		return nil, err
	}

	if err = r.spend(uint64(len(c)), stringHeaderBytes); err != nil {
		return nil, err
	}

	out := make([]string, len(c))
	for i, code := range c {
		if code >= uint64(len(dict)) {
			return nil, fmt.Errorf("%w: dictionary code %v out of range", ErrMalformed, code)
		}

		if err = r.spend(uint64(len(dict[code])), 1); err != nil {
			return nil, err
		}

		out[i] = dict[code]
	}

	return out, nil
}

// FSST replaces common byte sequences with one byte codes into a symbol table. Code 255 escapes a literal byte.
func fsstStrings(r *reader, symbolLengths, symbolTable, lengths, corpus stream) ([]string, error) {
	symLens, err := symbolLengths.unsigned(r, false)
	if err != nil {
		return nil, err
	}

	symbols, err := splitSymbols(symbolTable.data, symLens)
	if err != nil {
		return nil, err
	}

	size, err := fsstSize(corpus.data, symbols)
	if err != nil {
		return nil, err
	}

	if err = r.spend(size, 1); err != nil {
		return nil, err
	}

	expandedCorpus := make([]byte, 0, size)
	for i := 0; i < len(corpus.data); i++ {
		if code := corpus.data[i]; code == fsstEscape {
			i++
			expandedCorpus = append(expandedCorpus, corpus.data[i])
		} else {
			expandedCorpus = append(expandedCorpus, symbols[code]...)
		}
	}

	n, err := lengths.unsigned(r, false)
	if err != nil {
		return nil, err
	}

	return splitStrings(expandedCorpus, n)
}

const fsstEscape = 255

// Long symbols let a short corpus expand enormously, so the size is checked before expanding.
func fsstSize(corpus []byte, symbols [][]byte) (uint64, error) {
	var size uint64

	for i := 0; i < len(corpus); i++ {
		code := corpus[i]

		switch {
		case code == fsstEscape && i+1 < len(corpus):
			i++
			size++
		case code == fsstEscape:
			return 0, fmt.Errorf("%w: FSST data ends in an escape", ErrMalformed)
		case int(code) >= len(symbols):
			return 0, fmt.Errorf("%w: FSST symbol %v out of range", ErrMalformed, code)
		default:
			size += uint64(len(symbols[code]))
		}
	}

	return size, nil
}

func splitSymbols(table []byte, lengths []uint64) ([][]byte, error) {
	if len(lengths) > fsstEscape {
		return nil, fmt.Errorf("%w: %v FSST symbols", ErrMalformed, len(lengths))
	}

	out := make([][]byte, len(lengths))
	pos := uint64(0)

	for i, n := range lengths {
		if n > uint64(len(table))-pos {
			return nil, fmt.Errorf("%w: FSST symbol lengths overrun the table", ErrMalformed)
		}

		out[i] = table[pos : pos+n]
		pos += n
	}

	return out, nil
}

// The dictionary streams end with its data stream, then each child has its own present and code streams.
func decodeSharedDictColumn(r *reader, c *Column, children []columnMeta) error {
	count, err := r.varint32()
	if err != nil {
		return err
	}

	var dictStreams []stream
	for len(dictStreams) < stringFSST && uint32(len(dictStreams)) < count { // #nosec G115 -- bounded by stringFSST
		s, err := readStream(r, false)
		if err != nil {
			return err
		}

		dictStreams = append(dictStreams, s)
		if s.category == categoryData && (s.subtype == dataSingle || s.subtype == dataShared) {
			break
		}
	}

	var dict []string

	switch len(dictStreams) {
	case stringPlain:
		dict, err = plainStrings(r, dictStreams[0], dictStreams[1])
	case stringFSST:
		dict, err = fsstStrings(r, dictStreams[0], dictStreams[1], dictStreams[2], dictStreams[3])
	default:
		err = fmt.Errorf("%w: shared dictionary %v has %v dictionary streams", ErrMalformed, c.Name, len(dictStreams))
	}

	if err != nil {
		return err
	}

	expected := uint64(len(dictStreams))

	for _, m := range children {
		child, streams, err := decodeSharedDictChild(r, m, dict)
		if err != nil {
			return err
		}

		expected += streams
		c.Children = append(c.Children, child)
	}

	// Older encoders wrote a stream count one too high.
	if uint64(count) != expected && uint64(count) != expected+1 {
		return fmt.Errorf("%w: shared dictionary %v declares %v streams but has %v", ErrMalformed, c.Name, count, expected)
	}

	return nil
}

func decodeSharedDictChild(r *reader, m columnMeta, dict []string) (Column, uint64, error) {
	child := Column{Type: ColumnString, Name: m.name, Nullable: m.nullable()}

	count, err := r.varint32()
	if err != nil {
		return child, 0, err
	}

	if child.Present, err = readPresent(r, child.Nullable); err != nil {
		return child, 0, err
	}

	streams := uint64(1)
	if child.Nullable {
		streams++
	}

	if uint64(count) != streams {
		return child, 0, fmt.Errorf("%w: shared dictionary child %v has %v streams", ErrMalformed, m.name, count)
	}

	codes, err := readStream(r, false)
	if err != nil {
		return child, 0, err
	}

	values, err := lookupDictionary(r, codes, dict)
	if err != nil {
		return child, 0, err
	}

	child.Values, err = expand(r, child.Present, values)

	return child, streams, err
}

// Every column must describe the same features as the geometry column.
func (l Layer) validate() error {
	n := l.FeatureCount()

	if l.ID != nil && !l.ID.fits(n) {
		return fmt.Errorf("%w: id column doesn't match the %v features of layer %v", ErrMalformed, n, l.Name)
	}

	for _, c := range l.Columns {
		if !c.fits(n) {
			return fmt.Errorf("%w: column %v doesn't match the %v features of layer %v", ErrMalformed, c.Name, n, l.Name)
		}
	}

	return nil
}

func (id IDColumn) fits(n int) bool {
	tooLong := func(v uint64) bool { return v > math.MaxUint32 }

	return columnFits(n, id.Nullable, id.Present, len(id.Values)) && (id.Long || !slices.ContainsFunc(id.Values, tooLong))
}

func (c Column) fits(n int) bool {
	if c.Type == ColumnSharedDict {
		return c.Values == nil && !slices.ContainsFunc(c.Children, func(child Column) bool {
			return child.Type != ColumnString || !child.fits(n)
		})
	}

	return valuesMatchType(c.Type, c.Values) && columnFits(n, c.Nullable, c.Present, valuesLen(c.Values))
}

func valuesMatchType(t ColumnType, values any) bool {
	switch values.(type) {
	case []bool:
		return t == ColumnBool
	case []int32:
		return t == ColumnInt32
	case []uint32:
		return t == ColumnUint32
	case []int64:
		return t == ColumnInt64
	case []uint64:
		return t == ColumnUint64
	case []float32:
		return t == ColumnFloat
	case []float64:
		return t == ColumnDouble
	case []string:
		return t == ColumnString
	default:
		return false
	}
}

func columnFits(n int, nullable bool, present []bool, values int) bool {
	return values == n && (!nullable || len(present) == n) && (nullable || present == nil)
}

func valuesLen(values any) int {
	switch v := values.(type) {
	case []bool:
		return len(v)
	case []int32:
		return len(v)
	case []uint32:
		return len(v)
	case []int64:
		return len(v)
	case []uint64:
		return len(v)
	case []float32:
		return len(v)
	case []float64:
		return len(v)
	case []string:
		return len(v)
	default:
		return -1
	}
}
