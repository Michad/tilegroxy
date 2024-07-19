package pkg

import "github.com/Michad/tilegroxy/internal/config"

type EntityType int

const (
	EntityAuth = iota
	EntityProvider
	EntityCache
	EntitySecret
)

type Entity[Config any] interface {
}

type EntityRegistration[Config any, T Entity[Config]] interface {
	Name() string
	Initialize(config Config, errorMessages config.ErrorMessages) (*T, error)
	InitializeConfig() Config
}

var registrations map[EntityType]map[string]interface{} //= make(map[string]interface{})

func Register[C any, T Entity[C]](entity EntityType, reg EntityRegistration[C, T]) {
	registrations[entity][reg.Name()] = reg
}

func Registration[C any, T Entity[C]](entity EntityType, name string) (EntityRegistration[C, T], bool) {
	o, ok := registrations[entity][name]

	if ok {
		return o.(EntityRegistration[C, T]), true
	}
	return nil, false
}
