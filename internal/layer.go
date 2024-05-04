package internal

import (
	"errors"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/providers"
)

type Layer struct {
	Id          string
	Config      config.Layer
	Provider    providers.Provider
	authContext *providers.AuthContext
	authMutex   sync.Mutex
}

func (l *Layer) authWithProvider() error {
	var err error

	l.authMutex.Lock()
	if l.authContext == nil || l.authContext.Expiration.Before(time.Now()) {
		err = l.Provider.Preauth(l.authContext)
	}
	l.authMutex.Unlock()

	return err
}

func (l *Layer) RenderTile(z int, x int, y int) (*providers.Image, error) {
	var img *providers.Image
	var err error
	if l.authContext == nil || l.authContext.Expiration.Before(time.Now()) {
		err = l.authWithProvider()
	}

	if err != nil {
		return nil, err
	}

	img, err = l.Provider.GenerateTile(*l.authContext, z, x, y)

	var authError *providers.AuthError
	if errors.As(err, &authError) {
		err = l.authWithProvider()

		if err != nil {
			return nil, err
		}

		img, err = l.Provider.GenerateTile(*l.authContext, z, x, y)

		if err != nil {
			return nil, err
		}
	}

	return img, nil
}
