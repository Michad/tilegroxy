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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/Michad/tilegroxy/pkg"
)

// Guards against files from versions that enumerated tiles in a different order, making the position meaningless
const progressVersion = 1

// Resuming would seed the wrong tiles, so the caller must reuse the same arguments or start over
var ErrProgressMismatch = errors.New("the progress file describes a different seed run")

// How far a seed got. A position only means the same tile when the layer, bounds and zooms match
type Progress struct {
	Version   int        `json:"version"`
	LayerName string     `json:"layer"`
	Bounds    pkg.Bounds `json:"bounds"`
	Zooms     []uint     `json:"zooms"`
	Total     uint64     `json:"total"`
	Position  uint64     `json:"position"`
}

func NewProgress(layerName string, e *SeedJob) *Progress {
	return &Progress{
		Version:   progressVersion,
		LayerName: layerName,
		Bounds:    e.Bounds(),
		Zooms:     e.Zooms(),
		Total:     e.Count(),
	}
}

// A missing file just means nothing to resume, so both return values are nil
func LoadProgress(path string) (*Progress, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- the path is operator supplied
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	var p Progress
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unable to parse progress file %v: %w", path, err)
	}

	if p.Version != progressVersion {
		return nil, fmt.Errorf("progress file %v was written by an incompatible version of tilegroxy", path)
	}

	return &p, nil
}

// Any change to the enumeration, including bounds and zoom levels, makes a recorded position refer to a different tile
func (p *Progress) Matches(layerName string, e *SeedJob) bool {
	return p.LayerName == layerName &&
		p.Bounds == e.Bounds() &&
		p.Total == e.Count() &&
		slices.Equal(p.Zooms, e.Zooms())
}

// Writes via a temp file so a seed killed mid-write leaves the previous position, not a truncated file
func (p *Progress) Save(path string, out io.Writer, verbose bool) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}

	tmpName := tmp.Name()

	if _, err = tmp.Write(raw); err != nil {
		//#nosec G104 -- Already failing, the remove below is what matters
		tmp.Close() //nolint:errcheck // Already failing, the remove below is what matters
		//#nosec G104 -- Nothing actionable if the temp file can't be cleaned up
		os.Remove(tmpName) //nolint:errcheck // Nothing actionable if the temp file can't be cleaned up

		fmt.Fprintf(out, "Error writing progress file: %s", err)

		return err
	}

	if err = tmp.Close(); err != nil {
		//#nosec G104 -- Nothing actionable if the temp file can't be cleaned up
		os.Remove(tmpName) //nolint:errcheck // Nothing actionable if the temp file can't be cleaned up

		fmt.Fprintf(out, "Error closing progress file after writing: %s", err)

		return err
	}

	if verbose {
		fmt.Fprint(out, "Saved progress file\n")
	}

	return os.Rename(tmpName, path)
}

func (p *Progress) Finish(path string, out io.Writer, verbose bool) error {
	if verbose {
		fmt.Fprint(out, "Removing progress file\n")
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}
