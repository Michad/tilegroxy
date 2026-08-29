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

package seed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Progress_RoundTrip(t *testing.T) {
	e, err := NewSeedJob("osm", world(), []uint{0, 1, 2})
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "progress.json")

	p := NewProgress("osm", e)
	p.Position = 7
	require.NoError(t, p.Save(path, os.Stdout, false))

	loaded, err := LoadProgress(path)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, uint64(7), loaded.Position)
	assert.Equal(t, e.Count(), loaded.Total)
	assert.Equal(t, "osm", loaded.LayerName)
	assert.True(t, loaded.Matches("osm", e))
}

// Nothing to resume from is a normal state for a first run, not a failure.
func Test_Progress_LoadMissingFile(t *testing.T) {
	loaded, err := LoadProgress(filepath.Join(t.TempDir(), "absent.json"))

	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func Test_Progress_LoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0600))

	_, err := LoadProgress(path)
	require.Error(t, err)
}

// A file from a version that enumerated tiles differently has a position that means something else.
func Test_Progress_LoadWrongVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":9999,"layer":"osm"}`), 0600))

	_, err := LoadProgress(path)
	require.ErrorContains(t, err, "incompatible version")
}

// A position only refers to the same tile when the enumeration it indexes into is identical, so
// anything that changes the sequence has to be caught.
func Test_Progress_MatchesDetectsChangedRun(t *testing.T) {
	e, err := NewSeedJob("osm", world(), []uint{0, 1, 2})
	require.NoError(t, err)

	p := NewProgress("osm", e)

	differentZoom, err := NewSeedJob("osm", world(), []uint{0, 1, 3})
	require.NoError(t, err)
	assert.False(t, p.Matches("osm", differentZoom))

	extraZoom, err := NewSeedJob("osm", world(), []uint{0, 1, 2, 3})
	require.NoError(t, err)
	assert.False(t, p.Matches("osm", extraZoom))

	differentBounds, err := NewSeedJob("osm", pkg.Bounds{South: 10, North: 20, West: 10, East: 20, SRID: pkg.SRIDWGS84}, []uint{0, 1, 2})
	require.NoError(t, err)
	assert.False(t, p.Matches("osm", differentBounds))

	assert.False(t, p.Matches("other", e))
	assert.True(t, p.Matches("osm", e))
}

// Saving repeatedly over a run has to leave one usable file behind rather than accumulating
// leftovers from the temporary file each write goes through.
func Test_Progress_SaveOverwritesCleanly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress.json")

	e, err := NewSeedJob("osm", world(), []uint{1})
	require.NoError(t, err)

	p := NewProgress("osm", e)

	for i := range uint64(3) {
		p.Position = i
		require.NoError(t, p.Save(path, os.Stdout, false))
	}

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	loaded, err := LoadProgress(path)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), loaded.Position)
}

func Test_Progress_SaveToUnwritableLocation(t *testing.T) {
	e, err := NewSeedJob("osm", world(), []uint{1})
	require.NoError(t, err)

	err = NewProgress("osm", e).Save(filepath.Join(t.TempDir(), "no-such-dir", "progress.json"), os.Stdout, false)
	require.Error(t, err)
}
