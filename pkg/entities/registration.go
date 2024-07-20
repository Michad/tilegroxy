// Copyright 2024 Michael Davis
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

package entities

import "github.com/Michad/tilegroxy/pkg/config"

type EntityType int

const (
	EntityAuth = iota
	EntityProvider
	EntityCache
	EntitySecret
)

type EntityRegistration[T any] interface {
	Name() string
	Initialize(config any, errorMessages config.ErrorMessages) (T, error)
	InitializeConfig() any
}

var registrations map[EntityType]map[string]interface{} = make(map[EntityType]map[string]interface{})

func init() {
	for i := EntityAuth; i <= EntitySecret; i++ {
		registrations[EntityType(i)] = make(map[string]interface{})
	}
}

func Register[T any](entity EntityType, reg EntityRegistration[T]) {
	registrations[entity][reg.Name()] = reg
}

func Registration[T any](entity EntityType, name string) (EntityRegistration[T], bool) {
	o, ok := registrations[entity][name]

	if ok {
		return o.(EntityRegistration[T]), true
	}
	return nil, false
}
