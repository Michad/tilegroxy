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
	"fmt"
	"math"

	"github.com/paulmach/orb"
)

type geometryType uint8

const (
	typePoint geometryType = iota
	typeLineString
	typePolygon
	typeMultiPoint
	typeMultiLineString
	typeMultiPolygon
)

func (t geometryType) isMulti() bool {
	return t > typePolygon
}

func (t geometryType) isPolygon() bool {
	return t == typePolygon || t == typeMultiPolygon
}

func (t geometryType) isLine() bool {
	return t == typeLineString || t == typeMultiLineString
}

type lengths struct {
	values []uint64
	pos    int
}

func (l *lengths) present() bool {
	return l.values != nil
}

func (l *lengths) next() (int, error) {
	if l.pos >= len(l.values) {
		return 0, fmt.Errorf("%w: geometry topology runs out of lengths", ErrMalformed)
	}

	v := l.values[l.pos]
	l.pos++

	if v > math.MaxInt32 {
		return 0, fmt.Errorf("%w: geometry length %v too large", ErrMalformed, v)
	}

	return int(v), nil
}

type geometryStreams struct {
	types                  []uint64
	geometries, parts      lengths
	rings                  lengths
	vertices               []int32
	vertexOffsets          []uint64
	hasIndexBuffer         bool
	vertexPos, vertexCount int
}

// The first stream holds each feature's geometry type. The rest are identified by their stream type.
func decodeGeometryColumn(r *reader) ([]orb.Geometry, error) {
	count, err := r.varint32()
	if err != nil {
		return nil, err
	}

	if count == 0 {
		return nil, fmt.Errorf("%w: geometry column without streams", ErrMalformed)
	}

	var g geometryStreams

	typeStream, err := readStream(r, false)
	if err != nil {
		return nil, err
	}

	if g.types, err = typeStream.unsigned(r, false); err != nil {
		return nil, err
	}

	for range count - 1 {
		s, err := readStream(r, false)
		if err != nil {
			return nil, err
		}

		if err = g.read(r, s); err != nil {
			return nil, err
		}
	}

	if err = g.resolveVertices(); err != nil {
		return nil, err
	}

	return g.build()
}

func (g *geometryStreams) read(r *reader, s stream) error {
	var err error

	switch {
	case s.is(categoryLength, lengthGeometries):
		g.geometries.values, err = s.unsigned(r, false)
	case s.is(categoryLength, lengthParts):
		g.parts.values, err = s.unsigned(r, false)
	case s.is(categoryLength, lengthRings):
		g.rings.values, err = s.unsigned(r, false)
	case s.is(categoryOffset, offsetVertex):
		g.vertexOffsets, err = s.unsigned(r, false)
	case s.is(categoryOffset, offsetIndex):
		g.hasIndexBuffer = true
	case s.is(categoryData, dataVertex), s.is(categoryData, dataMorton):
		g.vertices, err = s.vertices(r)
	case s.is(categoryLength, lengthTriangles), s.category == categoryPresent:
		// Pre-tessellated triangles are rebuilt by clients from the outlines, so they're dropped.
	default:
		err = fmt.Errorf("%w: unexpected stream %v/%v in geometry column", ErrMalformed, s.category, s.subtype)
	}

	return err
}

// A vertex dictionary lists distinct vertices once, with offsets naming the one used at each position.
func (g *geometryStreams) resolveVertices() error {
	if len(g.vertices)%2 != 0 {
		return fmt.Errorf("%w: odd number of vertex coordinates", ErrMalformed)
	}

	if g.vertexOffsets == nil {
		g.vertexCount = len(g.vertices) / 2
		return nil
	}

	dict := g.vertices
	g.vertices = make([]int32, 2*len(g.vertexOffsets))

	for i, o := range g.vertexOffsets {
		if o >= uint64(len(dict)/2) {
			return fmt.Errorf("%w: vertex offset %v out of range", ErrMalformed, o)
		}

		g.vertices[2*i], g.vertices[2*i+1] = dict[2*o], dict[2*o+1]
	}

	g.vertexCount = len(g.vertexOffsets)

	return nil
}

func (g *geometryStreams) build() ([]orb.Geometry, error) {
	if g.hasIndexBuffer && !g.parts.present() {
		return nil, fmt.Errorf("%w: tessellated polygons without outlines", ErrUnsupported)
	}

	out := make([]orb.Geometry, len(g.types))
	for i, raw := range g.types {
		if raw > uint64(typeMultiPolygon) {
			return nil, fmt.Errorf("%w: unknown geometry type %v", ErrMalformed, raw)
		}

		var err error
		if out[i], err = g.feature(geometryType(raw)); err != nil {
			return nil, err
		}
	}

	if g.vertexPos != g.vertexCount {
		return nil, fmt.Errorf("%w: %v vertices left over", ErrMalformed, g.vertexCount-g.vertexPos)
	}

	return out, nil
}

// Which stream holds each count depends on the column's mix of types, per the spec's Length Stream Encoding Rules.
func (g *geometryStreams) feature(t geometryType) (orb.Geometry, error) {
	count := 1
	if t.isMulti() {
		var err error
		if count, err = g.multiCount(); err != nil {
			return nil, err
		}
	}

	parts := make([][][]orb.Point, count)
	for i := range parts {
		var err error
		if parts[i], err = g.subGeometry(t); err != nil {
			return nil, err
		}
	}

	return assemble(t, parts), nil
}

// Without a geometries stream the count of a multi geometry is stored in the parts stream.
func (g *geometryStreams) multiCount() (int, error) {
	if g.geometries.present() {
		return g.geometries.next()
	}

	return g.parts.next()
}

// Returns the rings or lines of one polygon, line or point.
func (g *geometryStreams) subGeometry(t geometryType) ([][]orb.Point, error) {
	ringCount := 1
	if t.isPolygon() {
		if !g.parts.present() || !g.rings.present() {
			return nil, fmt.Errorf("%w: polygon without ring lengths", ErrMalformed)
		}

		if !t.isMulti() || g.geometries.present() {
			var err error
			if ringCount, err = g.parts.next(); err != nil {
				return nil, err
			}
		}
	}

	out := make([][]orb.Point, ringCount)
	for i := range out {
		n, err := g.vertexRunLength(t)
		if err != nil {
			return nil, err
		}

		if out[i], err = g.take(n); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// Lines and rings take their vertex count from the rings stream when present, otherwise from parts.
func (g *geometryStreams) vertexRunLength(t geometryType) (int, error) {
	switch {
	case !t.isLine() && !t.isPolygon():
		return 1, nil
	case g.rings.present():
		return g.rings.next()
	case g.parts.present() && t.isLine():
		return g.parts.next()
	default:
		return 0, fmt.Errorf("%w: line or polygon without vertex counts", ErrMalformed)
	}
}

func (g *geometryStreams) take(n int) ([]orb.Point, error) {
	if n > g.vertexCount-g.vertexPos {
		return nil, fmt.Errorf("%w: geometry needs more vertices than exist", ErrMalformed)
	}

	out := make([]orb.Point, n)
	for i := range out {
		v := 2 * (g.vertexPos + i)
		out[i] = orb.Point{float64(g.vertices[v]), float64(g.vertices[v+1])}
	}

	g.vertexPos += n

	return out, nil
}

func assemble(t geometryType, parts [][][]orb.Point) orb.Geometry {
	switch t {
	case typePoint:
		return parts[0][0][0]
	case typeLineString:
		return orb.LineString(parts[0][0])
	case typePolygon:
		return polygon(parts[0])
	case typeMultiPoint:
		mp := make(orb.MultiPoint, len(parts))
		for i, p := range parts {
			mp[i] = p[0][0]
		}

		return mp
	case typeMultiLineString:
		ml := make(orb.MultiLineString, len(parts))
		for i, p := range parts {
			ml[i] = p[0]
		}

		return ml
	case typeMultiPolygon:
	}

	mp := make(orb.MultiPolygon, len(parts))
	for i, p := range parts {
		mp[i] = polygon(p)
	}

	return mp
}

// MLT leaves rings open, orb closes them.
func polygon(rings [][]orb.Point) orb.Polygon {
	p := make(orb.Polygon, len(rings))
	for i, ring := range rings {
		if len(ring) > 0 && ring[0] != ring[len(ring)-1] {
			ring = append(ring, ring[0])
		}

		p[i] = ring
	}

	return p
}

type geometryEncoder struct {
	types, geometries, parts, rings []uint64
	vertices                        []int32
	hasPolygon                      bool
}

func appendGeometryColumn(buf []byte, geoms []orb.Geometry) ([]byte, error) {
	e := geometryEncoder{types: make([]uint64, len(geoms))}

	for i, g := range geoms {
		t, err := typeOf(g)
		if err != nil {
			return nil, err
		}

		e.types[i] = uint64(t)
		e.hasPolygon = e.hasPolygon || t.isPolygon()
	}

	for _, g := range geoms {
		if err := e.add(g); err != nil {
			return nil, err
		}
	}

	streams := [][]uint64{e.geometries, e.parts, e.rings}
	subtypes := []uint8{lengthGeometries, lengthParts, lengthRings}

	count := uint64(2)
	for _, s := range streams {
		if s != nil {
			count++
		}
	}

	buf = appendVarint(buf, count)
	// Types go in a VarBinary length stream, matching the reference encoders.
	buf = appendVarintStream(buf, categoryLength, lengthVarBinary, e.types)

	for i, s := range streams {
		if s != nil {
			buf = appendVarintStream(buf, categoryLength, subtypes[i], s)
		}
	}

	return appendVertexStream(buf, e.vertices), nil
}

func typeOf(g orb.Geometry) (geometryType, error) {
	switch g.(type) {
	case orb.Point:
		return typePoint, nil
	case orb.LineString:
		return typeLineString, nil
	case orb.Polygon:
		return typePolygon, nil
	case orb.MultiPoint:
		return typeMultiPoint, nil
	case orb.MultiLineString:
		return typeMultiLineString, nil
	case orb.MultiPolygon:
		return typeMultiPolygon, nil
	default:
		return 0, fmt.Errorf("%w: geometry type %T", ErrUnsupported, g)
	}
}

func (e *geometryEncoder) add(g orb.Geometry) error {
	switch v := g.(type) {
	case orb.Point:
		return e.addPoints(v)
	case orb.LineString:
		return e.addLine(v)
	case orb.Polygon:
		return e.addPolygon(v)
	case orb.MultiPoint:
		e.geometries = append(e.geometries, uint64(len(v)))
		return e.addPoints(v...)
	case orb.MultiLineString:
		e.geometries = append(e.geometries, uint64(len(v)))
		for _, l := range v {
			if err := e.addLine(l); err != nil {
				return err
			}
		}
	case orb.MultiPolygon:
		e.geometries = append(e.geometries, uint64(len(v)))
		for _, p := range v {
			if err := e.addPolygon(p); err != nil {
				return err
			}
		}
	}

	return nil
}

// A line's vertex count goes in rings when polygons share the column, otherwise in parts.
func (e *geometryEncoder) addLine(l orb.LineString) error {
	if e.hasPolygon {
		e.rings = append(e.rings, uint64(len(l)))
	} else {
		e.parts = append(e.parts, uint64(len(l)))
	}

	return e.addPoints(l...)
}

func (e *geometryEncoder) addPolygon(p orb.Polygon) error {
	e.parts = append(e.parts, uint64(len(p)))

	for _, ring := range p {
		if len(ring) > 1 && ring.Closed() {
			ring = ring[:len(ring)-1]
		}

		e.rings = append(e.rings, uint64(len(ring)))
		if err := e.addPoints(ring...); err != nil {
			return err
		}
	}

	return nil
}

func (e *geometryEncoder) addPoints(points ...orb.Point) error {
	for _, p := range points {
		x, errX := toCoordinate(p[0])
		y, errY := toCoordinate(p[1])

		if errX != nil || errY != nil {
			return fmt.Errorf("%w: coordinate %v outside the 32 bit range", ErrUnsupported, p)
		}

		e.vertices = append(e.vertices, x, y)
	}

	return nil
}

func toCoordinate(v float64) (int32, error) {
	r := math.Round(v)
	if r < math.MinInt32 || r > math.MaxInt32 || math.IsNaN(r) {
		return 0, ErrUnsupported
	}

	return int32(r), nil
}
