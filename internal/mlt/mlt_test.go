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
	"math"
	"os"
	"testing"

	"github.com/paulmach/orb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func square(minX, minY, maxX, maxY float64) orb.Polygon {
	return orb.Polygon{{{minX, minY}, {maxX, minY}, {maxX, maxY}, {minX, maxY}, {minX, minY}}}
}

// One feature of every geometry type, with every kind of column
func sampleLayer() Layer {
	present := []bool{true, false, true, true, false, true}

	return Layer{
		Name:   "sample",
		Extent: 4096,
		ID:     &IDColumn{Long: true, Nullable: true, Present: present, Values: []uint64{1, 0, math.MaxUint64, 4, 0, 6}},
		Geometries: []orb.Geometry{
			orb.Point{10, 20},
			orb.LineString{{0, 0}, {100, 100}, {200, 0}},
			orb.Polygon{square(0, 0, 1000, 1000)[0], square(100, 100, 200, 200)[0]},
			orb.MultiPoint{{1, 2}, {3, 4}},
			orb.MultiLineString{{{0, 0}, {5, 5}}, {{-10, -10}, {-20, 30}}},
			orb.MultiPolygon{square(0, 0, 10, 10), square(20, 20, 30, 30)},
		},
		Columns: []Column{
			{Type: ColumnBool, Name: "bool", Values: []bool{true, false, true, true, false, false}},
			{Type: ColumnInt32, Name: "i32", Nullable: true, Present: present, Values: []int32{-1, 0, math.MaxInt32, math.MinInt32, 0, 7}},
			{Type: ColumnUint32, Name: "u32", Values: []uint32{0, 1, 2, math.MaxUint32, 4, 5}},
			{Type: ColumnInt64, Name: "i64", Values: []int64{math.MinInt64, -1, 0, 1, math.MaxInt64, 3}},
			{Type: ColumnUint64, Name: "u64", Nullable: true, Present: present, Values: []uint64{math.MaxUint64, 0, 2, 3, 0, 5}},
			{Type: ColumnFloat, Name: "f32", Values: []float32{0.5, -1, float32(math.Inf(1)), 3.25, 0, 1e-30}},
			{Type: ColumnDouble, Name: "f64", Nullable: true, Present: present, Values: []float64{0.1, 0, -2.5, 1e300, 0, math.Inf(-1)}},
			{Type: ColumnString, Name: "str", Nullable: true, Present: present, Values: []string{"a", "", "ünïcode", "", "", "long value"}},
			{Type: ColumnSharedDict, Name: "name:", Children: []Column{
				{Type: ColumnString, Name: "en", Values: []string{"one", "two", "three", "four", "five", "six"}},
				{Type: ColumnString, Name: "de", Nullable: true, Present: present, Values: []string{"eins", "", "three", "vier", "", "one"}},
			}},
		},
	}
}

func Test_Encode_RoundTripsEveryType(t *testing.T) {
	layers := []Layer{sampleLayer(), {Name: "empty", Extent: 512, Geometries: []orb.Geometry{}}}

	data, err := Encode(layers)
	require.NoError(t, err)

	decoded, skipped, err := Decode(data)
	require.NoError(t, err)
	assert.Zero(t, skipped)
	assert.Equal(t, layers, decoded)
}

func Test_Encode_LinesWithoutPolygons(t *testing.T) {
	layer := Layer{Name: "lines", Extent: 4096, Geometries: []orb.Geometry{
		orb.LineString{{0, 0}, {1, 1}},
		orb.MultiLineString{{{2, 2}, {3, 3}, {4, 4}}},
		orb.MultiPoint{{5, 5}},
	}}

	data, err := Encode([]Layer{layer})
	require.NoError(t, err)

	decoded, _, err := Decode(data)
	require.NoError(t, err)
	assert.Equal(t, []Layer{layer}, decoded)
}

func Test_Encode_RejectsInvalidLayers(t *testing.T) {
	tests := map[string]func(*Layer){
		"no name":          func(l *Layer) { l.Name = "" },
		"no extent":        func(l *Layer) { l.Extent = 0 },
		"short column":     func(l *Layer) { l.Columns[0].Values = []bool{true} },
		"wrong value type": func(l *Layer) { l.Columns[0].Values = []string{"a", "b", "c", "d", "e", "f"} },
		"missing present":  func(l *Layer) { l.Columns[1].Present = nil },
		"present when not nullable": func(l *Layer) {
			l.Columns[0].Present = []bool{true, true, true, true, true, true}
		},
		"wide 32 bit id":    func(l *Layer) { l.ID.Long = false },
		"bad child":         func(l *Layer) { l.Columns[8].Children[0].Type = ColumnInt32 },
		"shared dict value": func(l *Layer) { l.Columns[8].Values = []string{} },
		"unsupported geometry": func(l *Layer) {
			l.Geometries[0] = orb.Collection{orb.Point{1, 1}}
		},
		"coordinate out of range": func(l *Layer) { l.Geometries[0] = orb.Point{math.MaxInt64, 0} },
		"NaN coordinate":          func(l *Layer) { l.Geometries[1] = orb.LineString{{math.NaN(), 0}, {1, 1}} },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			l := sampleLayer()
			mutate(&l)

			_, err := Encode([]Layer{l})
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrMalformed) || errors.Is(err, ErrUnsupported), err.Error())
		})
	}
}

func Test_ByteRle_RoundTrips(t *testing.T) {
	long := make([]byte, 300)
	mixed := make([]byte, 0, 400)

	for i := range 400 {
		mixed = append(mixed, byte(i%7))
	}

	tests := [][]byte{{}, {1}, {1, 1}, {1, 1, 1}, {1, 2, 3, 3, 3, 3, 4}, long, mixed}
	for _, src := range tests {
		decoded, err := decodeByteRle(encodeByteRle(src), uint64(len(src)))
		require.NoError(t, err)
		assert.Equal(t, src, decoded)
	}
}

func Test_Select_KeepsFeaturesAligned(t *testing.T) {
	l := sampleLayer().Select([]int{5, 0})

	assert.Equal(t, 2, l.FeatureCount())
	assert.Equal(t, []uint64{6, 1}, l.ID.Values)
	assert.Equal(t, []bool{true, true}, l.ID.Present)
	assert.Equal(t, []int32{7, -1}, l.Columns[1].Values)
	assert.Nil(t, l.Columns[0].Present)
	assert.Equal(t, []string{"one", "eins"}, l.Columns[8].Children[1].Values)
}

func Test_Clip(t *testing.T) {
	l := Layer{Name: "clip", Extent: 100, Geometries: []orb.Geometry{
		orb.Point{10, 10},
		orb.Point{90, 90},
		orb.LineString{{0, 25}, {100, 25}},
		square(40, 40, 60, 60),
		orb.MultiPoint{{1, 1}, {80, 80}},
		orb.MultiLineString{{{80, 80}, {90, 90}}, {{0, 10}, {10, 10}}},
		orb.MultiPolygon{square(0, 0, 10, 10), square(80, 80, 90, 90)},
		orb.Polygon{square(0, 0, 49.8, 49.8)[0], square(49.9, 49.9, 50, 50)[0]},
		orb.LineString{{49.6, 0}, {49.6, 49.6}},
	}}
	l.Columns = []Column{{Type: ColumnUint32, Name: "n", Values: []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8}}}

	clipped := l.Clip(orb.Bound{Min: orb.Point{0, 0}, Max: orb.Point{50, 50}})

	assert.Equal(t, []uint32{0, 2, 3, 4, 5, 6, 7, 8}, clipped.Columns[0].Values)

	expected := []orb.Geometry{
		orb.Point{10, 10},
		orb.LineString{{0, 25}, {50, 25}},
		square(40, 40, 50, 50),
		orb.Point{1, 1},
		orb.LineString{{0, 10}, {10, 10}},
		square(0, 0, 10, 10),
		square(0, 0, 50, 50),
		orb.LineString{{50, 0}, {50, 50}},
	}
	require.Len(t, clipped.Geometries, len(expected))

	// Clipping may start a ring at a different vertex, so shapes are compared by type and extent
	for i, g := range clipped.Geometries {
		assert.Equal(t, expected[i].GeoJSONType(), g.GeoJSONType(), "geometry %v", i)
		assert.Equal(t, expected[i].Bound(), g.Bound(), "geometry %v", i)
	}

	assert.Len(t, clipped.Geometries[6], 1, "the collapsed hole is removed")
}

func Test_Clip_DropsCollapsedGeometry(t *testing.T) {
	bound := orb.Bound{Min: orb.Point{0, 0}, Max: orb.Point{10, 10}}

	assert.Nil(t, snap(orb.LineString{{1, 1}, {1.2, 1.2}}))
	assert.Nil(t, snap(orb.MultiLineString{{{1, 1}, {1.2, 1.2}}}))
	assert.Nil(t, snap(orb.MultiPolygon{square(1, 1, 1.2, 1.2)}))
	assert.Nil(t, snap(orb.MultiPoint{}))
	assert.Nil(t, snap(orb.Collection{}))
	assert.Equal(t, 0, Layer{Name: "l", Extent: 1, Geometries: []orb.Geometry{orb.Point{20, 20}}}.Clip(bound).FeatureCount())
}

func Test_Decode_Empty(t *testing.T) {
	layers, skipped, err := Decode(nil)
	require.NoError(t, err)
	assert.Empty(t, layers)
	assert.Zero(t, skipped)
}

func Test_Decode_SkipsOtherLayerFormats(t *testing.T) {
	v1, err := Encode([]Layer{{Name: "a", Extent: 1, Geometries: []orb.Geometry{orb.Point{1, 1}}}})
	require.NoError(t, err)

	data := append([]byte{3, 2, 0xAA, 0xBB}, v1...)

	layers, skipped, err := Decode(data)
	require.NoError(t, err)
	assert.Len(t, layers, 1)
	assert.Equal(t, 1, skipped)
}

// A tiny tile can claim billions of values through run lengths
func excessiveRunsLayer() []byte {
	body := appendString(nil, "l")
	body = append(body, 1, 1, codeGeometry, 2)
	body = append(body, streamType(categoryLength, lengthVarBinary), logicalRle|physicalVarint, 2, 6, 1, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F)
	body = append(body, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F, 0)
	body = appendVertexStream(body, nil)

	return layerBytes(body)
}

// Each use of a dictionary entry is charged, since encoding writes it out again
func repeatedDictionaryLayer() []byte {
	entry := make([]byte, 1<<20)
	codes := appendVarint(appendVarint(nil, 60), 0)

	return withColumn([]byte{byte(ColumnString), 1, 's'}, []byte{3},
		appendVarintStream(nil, categoryLength, lengthDictionary, []uint64{uint64(len(entry))}),
		append(appendStreamHeader(nil, categoryOffset, offsetString, logicalRle|physicalVarint, 2, uint64(len(codes))), append([]byte{1, 60}, codes...)...),
		appendRawStream(nil, categoryData, dataSingle, 1, entry))
}

// A short FSST corpus of long symbols expands enormously
func fsstBombLayer() []byte {
	symbol := make([]byte, 1<<16)
	corpus := make([]byte, 1000)

	return withColumn([]byte{byte(ColumnString), 1, 's'}, []byte{4},
		appendVarintStream(nil, categoryLength, lengthSymbol, []uint64{uint64(len(symbol))}),
		appendRawStream(nil, categoryData, dataFSST, 1, symbol),
		appendVarintStream(nil, categoryLength, lengthDictionary, []uint64{uint64(len(symbol) * len(corpus))}),
		appendRawStream(nil, categoryData, dataSingle, 1, corpus))
}

func Test_Decode_SkipsLayersPastTheTileLimit(t *testing.T) {
	small := Layer{Name: "small", Extent: 1, Geometries: []orb.Geometry{orb.Point{1, 1}}}
	good, err := Encode([]Layer{small})
	require.NoError(t, err)

	tests := map[string][]byte{
		"run lengths":         excessiveRunsLayer(),
		"repeated dictionary": repeatedDictionaryLayer(),
		"FSST expansion":      fsstBombLayer(),
	}

	for name, bad := range tests {
		t.Run(name, func(t *testing.T) {
			layers, skipped, err := Decode(append(append(append([]byte{}, good...), bad...), good...))
			require.NoError(t, err)
			assert.Equal(t, []Layer{small, small}, layers)
			assert.Equal(t, 1, skipped)
		})
	}
}

func Test_Decode_DropsOversizedFeatures(t *testing.T) {
	line := make(orb.LineString, maxFeatureBytes/pointBytes+1)
	for i := range line {
		line[i] = orb.Point{float64(i % 4096), float64(i / 4096)}
	}

	l := Layer{Name: "big", Extent: 4096,
		Geometries: []orb.Geometry{orb.Point{1, 1}, line, orb.MultiLineString{{{0, 0}, {1, 1}}, line}, orb.Point{2, 2}},
		Columns:    []Column{{Type: ColumnUint32, Name: "n", Values: []uint32{0, 1, 2, 3}}},
	}

	data, err := Encode([]Layer{l})
	require.NoError(t, err)

	layers, skipped, err := Decode(data)
	require.NoError(t, err)
	assert.Zero(t, skipped)
	require.Len(t, layers, 1)
	assert.Equal(t, []orb.Geometry{orb.Point{1, 1}, orb.Point{2, 2}}, layers[0].Geometries)
	assert.Equal(t, []uint32{0, 3}, layers[0].Columns[0].Values)
}

func Test_DropOversized_CountsProperties(t *testing.T) {
	huge := string(make([]byte, maxFeatureBytes))
	l := Layer{Name: "l", Extent: 1,
		ID:         &IDColumn{Values: []uint64{1, 2, 3}},
		Geometries: []orb.Geometry{orb.Point{0, 0}, orb.Point{1, 1}, orb.Point{2, 2}},
		Columns: []Column{
			{Type: ColumnString, Name: "s", Values: []string{"a", huge, "c"}},
			{Type: ColumnSharedDict, Name: "d", Children: []Column{{Type: ColumnString, Name: "x", Values: []string{huge, "b", "c"}}}},
			{Type: ColumnBool, Name: "b", Values: []bool{true, false, true}},
		},
	}

	kept := l.dropOversized([]int{pointBytes, pointBytes, pointBytes})
	assert.Equal(t, []uint64{3}, kept.ID.Values)
	assert.Equal(t, []string{"c"}, kept.Columns[0].Values)
	assert.Equal(t, 1, l.dropOversized([]int{maxFeatureBytes, 0, 0}).FeatureCount(), "geometry and properties add up")
}

func Test_ExpandRuns_RejectsOverflowingRuns(t *testing.T) {
	_, err := expandRuns(newReader(nil), []uint64{1, math.MaxUint64, 1, 7, 8, 9}, 3, 1)
	require.ErrorIs(t, err, ErrMalformed)
}

func Test_FastPFOR_RejectsMoreExceptionsThanValues(t *testing.T) {
	d := fastPFORDecoder{words: []uint32{fastPFORBlockSize + 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}}

	_, _, err := d.exceptionStreams(1<<1, 0, fastPFORBlockSize)
	require.ErrorIs(t, err, ErrMalformed)
}

func layerBytes(body []byte) []byte {
	return append(appendVarint(nil, uint64(len(body))+1), append([]byte{tagV1}, body...)...)
}

func pointLayer(columns ...byte) []byte {
	body := appendString(nil, "l")
	body = append(body, 1)

	return append(body, columns...)
}

// One point and a single property column
func withColumn(meta []byte, data ...[]byte) []byte {
	body := pointLayer(append([]byte{2, codeGeometry}, meta...)...)
	body = append(body, appendGeometryColumnOrPanic([]orb.Geometry{orb.Point{1, 1}})...)

	for _, d := range data {
		body = append(body, d...)
	}

	return layerBytes(body)
}

// The geometry column holds the given types and extra streams
func withGeometry(types []uint64, streams int, extra ...func([]byte) []byte) []byte {
	buf := appendVarintStream([]byte{byte(streams)}, categoryLength, lengthVarBinary, types)
	for _, e := range extra {
		buf = e(buf)
	}

	return layerBytes(append(pointLayer(1, codeGeometry), buf...))
}

func assertMalformed(t *testing.T, tests map[string][]byte) {
	t.Helper()

	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := Decode(data)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrMalformed) || errors.Is(err, ErrUnsupported), err.Error())
		})
	}
}

func Test_Decode_RejectsMalformedLayers(t *testing.T) {
	geometry := appendGeometryColumnOrPanic([]orb.Geometry{orb.Point{1, 1}})

	assertMalformed(t, map[string][]byte{
		"zero size layer":  {0},
		"truncated layer":  {9, 1, 1},
		"empty name":       layerBytes([]byte{0, 1, 0}),
		"zero extent":      layerBytes([]byte{1, 'l', 0, 0}),
		"no geometry":      layerBytes(pointLayer(0)),
		"two geometries":   layerBytes(append(pointLayer(2, codeGeometry, codeGeometry), append(geometry, geometry...)...)),
		"unknown column":   layerBytes(pointLayer(1, 5)),
		"trailing bytes":   layerBytes(append(append(pointLayer(1, codeGeometry), geometry...), 0)),
		"too many columns": layerBytes(pointLayer(100, codeGeometry)),
		"too few values":   withColumn([]byte{byte(ColumnUint32), 1, 'u'}, []byte{0x10, 0x02, 0, 0}),
		"present too short": withColumn([]byte{byte(ColumnUint32) | 1, 1, 'u'},
			[]byte{0x00, 0x60, 1, 2, 0xFF, 0x01}, []byte{0x10, 0x02, 2, 2, 1, 1}),
	})
}

func Test_Decode_RejectsMalformedStreams(t *testing.T) {
	u32 := []byte{byte(ColumnUint32), 1, 'u'}

	assertMalformed(t, map[string][]byte{
		"bad stream category":   withColumn(u32, []byte{0x50, 0x02, 1, 1, 1}),
		"bad physical encoding": withColumn(u32, []byte{0x10, 0x03, 1, 1, 1}),
		"bad logical encoding":  withColumn(u32, []byte{0x10, 0xE2, 1, 1, 1}),
		"morton bits":           withColumn(u32, []byte{0x10, 0x82, 1, 1, 17, 0, 1}),
		"fixed width mismatch":  withColumn(u32, []byte{0x10, 0x00, 1, 3, 1, 2, 3}),
		"FastPFOR partial word": withColumn(u32, []byte{0x10, 0x01, 1, 3, 1, 2, 3}),
		"FastPFOR 64 bit":       withColumn([]byte{byte(ColumnUint64), 1, 'u'}, []byte{0x10, 0x01, 1, 4, 0, 0, 0, 0x81}),
		"varint count":          withColumn(u32, []byte{0x10, 0x02, 2, 1, 1}),
		"wide 32 bit varint":    withColumn(u32, []byte{0x10, 0x02, 1, 5, 0xFF, 0xFF, 0xFF, 0xFF, 0x7F}),
		"runs mismatch":         withColumn(u32, []byte{0x10, 0x62, 2, 2, 1, 2, 1, 5}),
		"float not plain":       withColumn([]byte{byte(ColumnFloat), 1, 'f'}, []byte{0x10, 0x02, 1, 1, 1}),
		"bool encoding":         withColumn([]byte{byte(ColumnBool), 1, 'b'}, []byte{0x10, 0x02, 1, 1, 1}),
		"bool runs":             withColumn([]byte{byte(ColumnBool), 1, 'b'}, []byte{0x10, 0x60, 20, 1, 0xFF}),
	})
}

func Test_Decode_RejectsMalformedStrings(t *testing.T) {
	str := []byte{byte(ColumnString), 1, 's'}
	emptyFSSTTable := []byte{0x35, 0x02, 0, 0, 0x15, 0x00, 0, 0}

	assertMalformed(t, map[string][]byte{
		"string streams": withColumn(str, []byte{1}, []byte{0x30, 0x02, 1, 1, 1}),
		"nullable string no streams": withColumn([]byte{byte(ColumnString) | 1, 1, 's'},
			[]byte{0}, []byte{0x00, 0x60, 1, 2, 0xFF, 0x01}),
		"string overrun":  withColumn(str, []byte{2}, []byte{0x30, 0x02, 1, 1, 5}, []byte{0x10, 0x00, 1, 1, 'x'}),
		"string underrun": withColumn(str, []byte{2}, []byte{0x30, 0x02, 1, 1, 0}, []byte{0x10, 0x00, 1, 1, 'x'}),
		"dictionary code": withColumn(str, []byte{3}, []byte{0x36, 0x02, 1, 1, 1}, []byte{0x22, 0x02, 1, 1, 4}, []byte{0x11, 0x00, 1, 1, 'x'}),
		"FSST escape at end": withColumn(str, []byte{4}, emptyFSSTTable,
			[]byte{0x36, 0x02, 1, 1, 1}, []byte{0x11, 0x00, 1, 1, 0xFF}),
		"FSST symbol": withColumn(str, []byte{4}, emptyFSSTTable,
			[]byte{0x36, 0x02, 1, 1, 1}, []byte{0x11, 0x00, 1, 1, 0}),
		"FSST table": withColumn(str, []byte{4}, []byte{0x35, 0x02, 1, 1, 9}, []byte{0x15, 0x00, 0, 0},
			[]byte{0x36, 0x02, 1, 1, 1}, []byte{0x11, 0x00, 1, 1, 0}),
		"shared dict child type": withColumn([]byte{byte(ColumnSharedDict), 1, 'd', 1, byte(ColumnUint32), 0}),
		"shared dict streams":    withColumn([]byte{byte(ColumnSharedDict), 1, 'd', 0}, []byte{1}, []byte{0x11, 0x00, 0, 0}),
		"shared dict count": withColumn([]byte{byte(ColumnSharedDict), 1, 'd', 0},
			[]byte{9}, []byte{0x36, 0x02, 0, 0}, []byte{0x12, 0x00, 0, 0}),
		"shared dict child streams": withColumn([]byte{byte(ColumnSharedDict), 1, 'd', 1, byte(ColumnString), 0},
			[]byte{3}, []byte{0x36, 0x02, 0, 0}, []byte{0x12, 0x00, 0, 0}, []byte{2}, []byte{0x22, 0x02, 1, 1, 0}),
	})
}

func Test_Decode_RejectsMalformedGeometry(t *testing.T) {
	vertices := func(v ...int32) func([]byte) []byte {
		return func(b []byte) []byte { return appendVertexStream(b, v) }
	}
	lengths := func(category, subtype uint8, v ...uint64) func([]byte) []byte {
		return func(b []byte) []byte { return appendVarintStream(b, category, subtype, v) }
	}

	assertMalformed(t, map[string][]byte{
		"geometry without streams": layerBytes(append(pointLayer(1, codeGeometry), 0)),
		"geometry type":            withGeometry([]uint64{9}, 2, vertices(1, 1)),
		"geometry stream":          withGeometry([]uint64{0}, 2, lengths(categoryLength, lengthSymbol)),
		"too few vertices":         withGeometry([]uint64{0, 0}, 2, vertices(1, 1)),
		"leftover vertices":        withGeometry([]uint64{0}, 2, vertices(1, 1, 2, 2)),
		"polygon without rings":    withGeometry([]uint64{uint64(typePolygon)}, 2, vertices()),
		"line without lengths":     withGeometry([]uint64{uint64(typeLineString)}, 2, vertices()),
		"vertex offset":            withGeometry([]uint64{0}, 3, lengths(categoryOffset, offsetVertex, 3), vertices(1, 1)),
		"tessellation only":        withGeometry([]uint64{uint64(typePolygon)}, 3, lengths(categoryOffset, offsetIndex, 0), vertices()),
		"huge multi count": withGeometry([]uint64{uint64(typeMultiPoint)}, 3,
			lengths(categoryLength, lengthGeometries, math.MaxInt32), vertices(1, 1)),
		"huge ring count": withGeometry([]uint64{uint64(typePolygon)}, 4,
			lengths(categoryLength, lengthParts, math.MaxInt32), lengths(categoryLength, lengthRings, 1), vertices(1, 1)),
	})
}

func appendGeometryColumnOrPanic(geoms []orb.Geometry) []byte {
	b, err := appendGeometryColumn(nil, geoms)
	if err != nil {
		panic(err)
	}

	return b
}

func Test_DecodeFastPFOR_TailOnly(t *testing.T) {
	// No full blocks, then 5 and 300 in FastPFOR's variable byte coding
	out, err := decodeFastPFOR([]byte{0, 0, 0, 0, 0x00, 0x82, 0x2C, 0x85}, 2)
	require.NoError(t, err)
	assert.Equal(t, []uint64{5, 300}, out)

	_, err = decodeFastPFOR([]byte{0, 0, 0, 0}, 1)
	require.ErrorIs(t, err, ErrMalformed)

	_, err = decodeFastPFOR([]byte{0, 0, 0, 7}, 7)
	require.ErrorIs(t, err, ErrMalformed)

	_, err = decodeFastPFOR([]byte{0, 0, 0, 0, 0x00, 0x00, 0x00, 0x00}, 1)
	require.ErrorIs(t, err, ErrMalformed)
}

// The embedded box matches box.mvt, so it must stay exactly what Encode writes
func Test_Encode_MatchesEmbeddedBox(t *testing.T) {
	embedded, err := os.ReadFile("../images/box.mlt")
	require.NoError(t, err)

	box := Layer{Name: "layer", Extent: 4096, Geometries: []orb.Geometry{
		orb.Polygon{{{4096, 0}, {4096, 4096}, {0, 4096}, {0, 0}, {4096, 0}}},
	}}

	encoded, err := Encode([]Layer{box})
	require.NoError(t, err)
	assert.Equal(t, embedded, encoded)
}
