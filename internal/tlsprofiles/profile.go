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

package tlsprofiles

import (
	"crypto/tls"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/config"
)

// Lowercase parameter names, tracked to warn about ones no reference uses
const (
	paramMinVersion = "minversion"
	paramHosts      = "hosts"
)

// Implemented by each kind. Callers switch on the concrete type for what's specific to it
type Profile interface {
	GetID() string
	base() *profileBase
}

// New dependencies are added as fields so the Initialize signature stays stable
type Deps struct {
	// The dotted config path for error messages
	Path          string
	ErrorMessages config.ErrorMessages
}

// Internal rather than in pkg/entities since profiles aren't a public extension point
type Registration interface {
	Name() string
	Initialize(config any, deps Deps) (Profile, error)
	InitializeConfig() any
}

var registrations = make(map[string]Registration)

func register(reg Registration) {
	registrations[reg.Name()] = reg
}

// What every kind has in common
type profileBase struct {
	ID         string
	MinVersion uint16
	Hosts      []string

	set        map[string]bool
	used       map[string]bool
	referenced bool
}

func newProfileBase(id, minVersion string, deps Deps) (profileBase, error) {
	version, err := parseMinVersion(minVersion, deps)
	if err != nil {
		return profileBase{}, err
	}

	return profileBase{ID: id, MinVersion: version, set: map[string]bool{}, used: map[string]bool{}}, nil
}

func (p *profileBase) GetID() string {
	return p.ID
}

func (p *profileBase) base() *profileBase {
	return p
}

func (p *profileBase) markUsed(params ...string) {
	p.referenced = true

	for _, param := range params {
		p.used[param] = true
	}
}

func parseMinVersion(raw string, deps Deps) (uint16, error) {
	switch raw {
	case "", "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	}

	return 0, fmt.Errorf(deps.ErrorMessages.EnumError, deps.Path+".minversion", raw, []string{"1.2", "1.3"})
}

func constructProfile(raw map[string]any, deps Deps) (Profile, error) {
	var name, id any

	// Viper keeps the original case of keys nested inside lists
	for k, v := range raw {
		switch strings.ToLower(k) {
		case "name":
			name = v
		case "id":
			id = v
		}
	}

	messages := deps.ErrorMessages

	if id == nil || id == "" {
		return nil, fmt.Errorf(messages.ParamRequired, deps.Path+".id")
	}

	if id == SystemID {
		return nil, fmt.Errorf(messages.InvalidParam, deps.Path+".id", id)
	}

	if name == nil || name == "" {
		return nil, fmt.Errorf(messages.ParamRequired, deps.Path+".name")
	}

	nameStr, _ := name.(string)

	reg, ok := registrations[nameStr]
	if !ok {
		return nil, fmt.Errorf(messages.EnumError, deps.Path+".name", name, slices.Sorted(maps.Keys(registrations)))
	}

	cfg := reg.InitializeConfig()
	if err := configload.DecodeEntityConfig(raw, &cfg); err != nil {
		return nil, fmt.Errorf(messages.InvalidParam, deps.Path, err)
	}

	return reg.Initialize(cfg, deps)
}
