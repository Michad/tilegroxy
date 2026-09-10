//go:build ignore

package custom

import (
	"os"
	"time"

	"tilegroxy/tilegroxy"
)

// This is intentionally silly
var password, readErr = os.ReadFile("/tmp/password")

func validate(token string) tilegroxy.ValidationResult {
	if readErr == nil && string(password) == token {
		return tilegroxy.ValidationResult{Pass: true, Expiration: time.Now().Add(1 * time.Hour), UserID: "user", AllowedLayers: []string{"osm"}}
	}

	return tilegroxy.ValidationResult{Pass: false, Expiration: time.Now().Add(1000 * time.Hour)}
}
