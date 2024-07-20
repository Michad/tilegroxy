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

type entityType int

const (
	entityAuth = iota
	entityProvider
	entityCache
	entitySecret
)

type EntityRegistration[T any] interface {
	Name() string
	Initialize(config any, errorMessages config.ErrorMessages) (T, error)
	InitializeConfig() any
}

var registrations map[entityType]map[string]interface{} = make(map[entityType]map[string]interface{})

func init() {
	for i := entityAuth; i <= entitySecret; i++ {
		registrations[entityType(i)] = make(map[string]interface{})
	}
}

func RegisterAuthentication(reg EntityRegistration[Authentication]) {
	registrations[entityAuth][reg.Name()] = reg
}

func RegisterProvider(reg EntityRegistration[Provider]) {
	registrations[entityProvider][reg.Name()] = reg
}

func RegisterCache(reg EntityRegistration[Cache]) {
	registrations[entityCache][reg.Name()] = reg
}
func RegisterSecret(reg EntityRegistration[Secreter]) {
	registrations[entitySecret][reg.Name()] = reg
}

func Registration[T any](name string) (EntityRegistration[T], bool) {
	for i := entityAuth; i <= entitySecret; i++ {
		o, ok := registrations[entityType(i)][name]

		if ok {
			e, ok := o.(EntityRegistration[T])
			if ok {
				return e, true
			}
		}
	}

	return nil, false
}
