package authentication

import "net/http"

type NoopAuth struct {
	Key string
}

func (c NoopAuth) Preauth(req *http.Request) bool {
	return true
}
