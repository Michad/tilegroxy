package authentication

import (
	"log"
	"net/http"
	"strings"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/google/uuid"
)

type StaticKeyConfig struct {
	Key string
}

type StaticKey struct {
	Config *StaticKeyConfig
}

func ConstructStaticKey(config *StaticKeyConfig, errorMessages *config.ErrorMessages) (*StaticKey, error) {
	if config.Key == "" {
		keyUuid, err := uuid.NewRandom()

		if err != nil {
			return nil, err
		}

		keyStr := strings.ReplaceAll(keyUuid.String(), "-", "")

		log.Printf("Generated authentication key: %v\n", keyStr)
		config.Key = keyStr
	}

	return &StaticKey{config}, nil
}

func (c StaticKey) Preauth(req *http.Request) bool {
	h := req.Header["Authorization"]
	return len(h) > 0 && h[0] == "Bearer "+c.Config.Key
}
