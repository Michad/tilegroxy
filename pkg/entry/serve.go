package tg

import (
	"io"

	"github.com/Michad/tilegroxy/internal/server"
	"github.com/Michad/tilegroxy/pkg/config"
)

type ServeOptions struct {
}

func Serve(cfg *config.Config, opts ServeOptions, out io.Writer) error {
	layerObjects, auth, err := ConfigToEntities(*cfg)
	if err != nil {
		return err
	}

	err = server.ListenAndServe(cfg, layerObjects, auth)
	return err
}
