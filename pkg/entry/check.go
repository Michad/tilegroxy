package tg

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Michad/tilegroxy/pkg/config"
)

type CheckOptions struct {
	Echo bool
}

func CheckConfig(cfg *config.Config, opts CheckOptions, out io.Writer) error {
	var err error
	_, _, err = configToEntities(*cfg)

	if err != nil {
		return err
	}

	if cfg != nil && opts.Echo {
		enc := json.NewEncoder(out)
		enc.SetIndent(" ", "  ")
		enc.Encode(cfg)
	} else {
		fmt.Fprintln(out, "Valid")
	}

	return nil
}
