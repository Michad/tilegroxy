package authentication

import (
	"errors"
	"fmt"
	"net/http"
)

type Authentication interface {
	Preauth(req *http.Request) bool
}

func ConstructAuth(rawConfig map[string]interface{}) (Authentication, error) {
	if rawConfig["name"] == "None" {
		return NoopAuth{}, nil
	}

	name := fmt.Sprintf("%#v", rawConfig["name"])
	return nil, errors.New("Unsupported auth " + name)
}
