package authentication

import "net/http"

type Noop struct {
	Key string
}

func (c Noop) Preauth(req *http.Request) bool {
	return true
}
