package providers

import (
	"fmt"
	"time"
)

type Image []byte

type Provider interface {
	Preauth(authContext *AuthContext) error
	GenerateTile(authContext AuthContext, z int, x int, y int) (*Image, error)
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
