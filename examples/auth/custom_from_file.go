package custom

import (
	"os"
	"time"
)

// This is intentionally silly
var password, readErr = os.ReadFile("/tmp/password")

func validate(token string) (bool, time.Time, string, []string) {
	if readErr == nil && string(password) == token {
		return true, time.Now().Add(1 * time.Hour), "user", []string{"osm"}
	}

	return false, time.Now().Add(1000 * time.Hour), "", []string{}
}
