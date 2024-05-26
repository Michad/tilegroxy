package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/layers"
)

type defaultHandler struct {
	config   *config.Config
	layerMap map[string]*layers.Layer
	auth     *authentication.Authentication
}

func (h *defaultHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	slog.Debug("server: default handler started")
	defer slog.Debug("server: default handler ended")

	select {
	case <-time.After(1 * time.Second):
		fmt.Fprintf(w, req.RequestURI+"\n")
	case <-ctx.Done():

		err := ctx.Err()
		slog.Debug("server:", err)
		internalError := http.StatusInternalServerError
		http.Error(w, err.Error(), internalError)
	}
}
