package providers

import (
	"errors"
	"fmt"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/mitchellh/mapstructure"
)

type Provider interface {
	Preauth(authContext *AuthContext) error
	GenerateTile(authContext AuthContext, z int, x int, y int) (*pkg.Image, error)
}

func ConstructProvider(rawConfig map[string]interface{}) (Provider, error) {

	if rawConfig["name"] == "url template" {
		var result UrlTemplate
		err := mapstructure.Decode(rawConfig, &result)
		return result, err
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, errors.New("Unsupported provider " + name)
}

type AuthContext struct {
	Expiration time.Time
	Token      string
	Other      map[string]interface{}
}

type AuthError struct {
	arg     int
	message string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("%d - %s", e.arg, e.message)
}
