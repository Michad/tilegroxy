package authentication

import "net/http"

type StaticKey struct {
	Key string
}

func (c StaticKey) Preauth(req *http.Request) bool {
	h := req.Header["Authentication"]
	return len(h) > 0 && h[0] == "Bearer "+c.Key
}
