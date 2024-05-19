package authentication

import (
	"fmt"
	"net/http"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/mitchellh/mapstructure"
)

type Authentication interface {
	Preauth(req *http.Request) bool
}

func ConstructAuth(rawConfig map[string]interface{}, errorMessages *config.ErrorMessages) (Authentication, error) {
	if rawConfig["name"] == "none" {
		return Noop{}, nil
	} else if rawConfig["name"] == "static key" {
		var config StaticKeyConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructStaticKey(&config, errorMessages)
	} else if rawConfig["name"] == "jwt" {
		var config JwtConfig
		err := mapstructure.Decode(rawConfig, &config)
		if err != nil {
			return nil, err
		}
		return ConstructJwt(&config, errorMessages)
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, fmt.Errorf(errorMessages.InvalidParam, "authentication.name", name)
}
