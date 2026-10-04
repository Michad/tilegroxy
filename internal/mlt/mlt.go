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

// Package mlt reads and writes MapLibre Tiles (MLT), the columnar vector tile format specified at
// https://github.com/maplibre/maplibre-tile-spec. Only the stable v1 layer format is supported.
package mlt

import (
	"errors"

	"github.com/paulmach/orb"
)

// ErrMalformed is wrapped by every error caused by a tile that doesn't follow the specification.
var ErrMalformed = errors.New("malformed MLT tile")

// ErrUnsupported is wrapped by errors for valid tile content this package can't process.
var ErrUnsupported = errors.New("unsupported MLT content")

// ColumnType is the type of a property column, without the nullable flag.
type ColumnType uint8

const (
	ColumnBool       ColumnType = 10
	ColumnInt32      ColumnType = 16
	ColumnUint32     ColumnType = 18
	ColumnInt64      ColumnType = 20
	ColumnUint64     ColumnType = 22
	ColumnFloat      ColumnType = 24
	ColumnDouble     ColumnType = 26
	ColumnString     ColumnType = 28
	ColumnSharedDict ColumnType = 30
)

// Layer is one decoded v1 layer. Every per-feature slice is indexed by feature.
type Layer struct {
	Name       string
	Extent     uint32
	ID         *IDColumn      // Nil if the layer has no id column
	Geometries []orb.Geometry // In tile coordinates. Polygon rings are closed
	Columns    []Column
}

// IDColumn holds feature ids.
type IDColumn struct {
	Long     bool     // Encoded as 64 bit rather than 32 bit ids
	Nullable bool     // Some features may lack an id
	Present  []bool   // Only set when Nullable
	Values   []uint64 // Zero where not present
}

// Column is a property column.
type Column struct {
	Type     ColumnType
	Name     string
	Nullable bool   // Some features may lack a value
	Present  []bool // Only set when Nullable
	// Per feature []bool, []int32, []uint32, []int64, []uint64, []float32, []float64 or []string matching Type. Zero where absent, nil for SharedDict
	Values any
	// ColumnSharedDict only. String columns sharing one dictionary, named by appending their Name to the parent's
	Children []Column
}

// FeatureCount is the number of features in the layer.
func (l Layer) FeatureCount() int {
	return len(l.Geometries)
}

// Select returns the layer reduced to the features at the given indexes, in the order given.
func (l Layer) Select(keep []int) Layer {
	out := l
	out.Geometries = pick(l.Geometries, keep)

	if l.ID != nil {
		out.ID = &IDColumn{Long: l.ID.Long, Nullable: l.ID.Nullable, Present: pickPresent(l.ID.Nullable, l.ID.Present, keep), Values: pick(l.ID.Values, keep)}
	}

	out.Columns = make([]Column, len(l.Columns))
	for i, c := range l.Columns {
		out.Columns[i] = c.selectRows(keep)
	}

	return out
}

func (c Column) selectRows(keep []int) Column {
	out := c
	out.Present = pickPresent(c.Nullable, c.Present, keep)

	switch v := c.Values.(type) {
	case []bool:
		out.Values = pick(v, keep)
	case []int32:
		out.Values = pick(v, keep)
	case []uint32:
		out.Values = pick(v, keep)
	case []int64:
		out.Values = pick(v, keep)
	case []uint64:
		out.Values = pick(v, keep)
	case []float32:
		out.Values = pick(v, keep)
	case []float64:
		out.Values = pick(v, keep)
	case []string:
		out.Values = pick(v, keep)
	}

	if c.Children != nil {
		out.Children = make([]Column, len(c.Children))
		for i, child := range c.Children {
			out.Children[i] = child.selectRows(keep)
		}
	}

	return out
}

func pickPresent(nullable bool, present []bool, keep []int) []bool {
	if !nullable {
		return nil
	}

	return pick(present, keep)
}

func pick[T any](values []T, keep []int) []T {
	out := make([]T, len(keep))
	for i, k := range keep {
		out[i] = values[k]
	}

	return out
}
