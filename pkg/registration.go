package pkg

import "github.com/Michad/tilegroxy/internal/config"

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
