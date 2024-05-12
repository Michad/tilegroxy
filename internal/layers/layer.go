package layers

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/Michad/tilegroxy/internal/caches"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/providers"
	"github.com/Michad/tilegroxy/pkg"
)

type Layer struct {
	Id          string
	Config      config.LayerConfig
	Provider    providers.Provider
	Cache       *caches.Cache
	authContext *providers.AuthContext
	authMutex   sync.Mutex
}

func ConstructLayer(rawConfig config.LayerConfig) (*Layer, error) {
	provider, error := providers.ConstructProvider(rawConfig.Provider)

	if error != nil {
		return nil, error
	}

	return &Layer{rawConfig.Id, rawConfig, provider, nil, nil, sync.Mutex{}}, nil
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

func (l *Layer) RenderTile(tileRequest pkg.TileRequest) (*pkg.Image, error) {
	var img *pkg.Image
	var err error

	img, err = (*l.Cache).Lookup(tileRequest)

	if img != nil {
		return img, err
	}

	if err != nil {
		log.Printf("Cache read error %v\n", err)
	}

	if l.authContext == nil || l.authContext.Expiration.Before(time.Now()) {
		err = l.authWithProvider()
	}

	if err != nil {
		return nil, err
	}

	img, err = l.Provider.GenerateTile(l.authContext, *l.Config.OverrideClient, tileRequest)

	var authError *providers.AuthError
	if errors.As(err, &authError) {
		err = l.authWithProvider()

		if err != nil {
			return nil, err
		}

		img, err = l.Provider.GenerateTile(l.authContext, *l.Config.OverrideClient, tileRequest)

		if err != nil {
			return nil, err
		}
	}

	err = (*l.Cache).Save(tileRequest, img)

	if err != nil {
		log.Printf("Cache save error %v\n", err)
	}

	return img, nil
}
