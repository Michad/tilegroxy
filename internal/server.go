package internal

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg"

	"github.com/gorilla/handlers"
)

type defaultHandler struct {
	config   config.Config
	layerMap map[string]*layers.Layer
	auth     *authentication.Authentication
}

type tileHandler struct {
	defaultHandler
}

func (h *defaultHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	fmt.Println("server: default handler started")
	defer fmt.Println("server: default handler ended")

	select {
	case <-time.After(1 * time.Second):
		fmt.Fprintf(w, req.RequestURI+"\n")
	case <-ctx.Done():

		err := ctx.Err()
		fmt.Println("server:", err)
		internalError := http.StatusInternalServerError
		http.Error(w, err.Error(), internalError)
	}
}

func handleNoContent(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func writeError(w http.ResponseWriter, cfg config.ErrorConfig, status int, message string, params ...any) {
	w.WriteHeader(status)

	fullMessage := fmt.Sprintf(message, params...)

	if cfg.Mode == config.ErrorPlainText {
		w.Write([]byte(fullMessage))
	} else {
		panic("TODO: other error modes")
	}
}

func (h *tileHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// ctx := req.Context()
	fmt.Println("server: tile handler started")
	defer fmt.Println("server: tile handler ended")

	if !(*h.auth).Preauth(req) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	layerName := req.PathValue("layer")
	zStr := req.PathValue("z")
	xStr := req.PathValue("x")
	yStr := req.PathValue("y")

	z, err := strconv.Atoi(zStr)

	if err != nil {
		writeError(w, h.config.Error, http.StatusBadRequest, h.config.Error.Messages.InvalidParam, "z", zStr)
		return
	}

	x, err := strconv.Atoi(xStr)

	if err != nil {
		writeError(w, h.config.Error, http.StatusBadRequest, h.config.Error.Messages.InvalidParam, "x", xStr)
		return
	}

	y, err := strconv.Atoi(yStr)

	if err != nil {
		writeError(w, h.config.Error, http.StatusBadRequest, h.config.Error.Messages.InvalidParam, "y", yStr)
		return
	}

	tileReq := pkg.TileRequest{LayerName: layerName, Z: z, X: x, Y: y}

	_, err = tileReq.GetBounds()

	if err != nil {
		var re pkg.RangeError
		if errors.As(err, &re) {
			writeError(w, h.config.Error, http.StatusBadRequest, h.config.Error.Messages.RangeError, re.ParamName, re.MinValue, re.MaxValue)
		} else {
			writeError(w, h.config.Error, http.StatusInternalServerError, h.config.Error.Messages.ServerError, err)
		}
		return
	}

	if h.layerMap[layerName] == nil {
		writeError(w, h.config.Error, http.StatusBadRequest, h.config.Error.Messages.InvalidParam, "layer", layerName)
		return
	}

	layer := h.layerMap[layerName]

	img, err := layer.RenderTile(tileReq)

	if err != nil {
		writeError(w, h.config.Error, http.StatusInternalServerError, h.config.Error.Messages.ServerError, err)
		return
	}

	if img == nil {
		writeError(w, h.config.Error, http.StatusInternalServerError, h.config.Error.Messages.ProviderError)
		return
	}

	w.WriteHeader(http.StatusOK)

	for h, v := range h.config.Server.StaticHeaders {
		w.Header().Add(h, v)
	}

	w.Write(*img)
}

func ListenAndServe(config config.Config, layerList []*layers.Layer, auth *authentication.Authentication) {
	r := http.ServeMux{}

	layerMap := make(map[string]*layers.Layer)
	for _, l := range layerList {
		layerMap[l.Id] = l
	}

	if config.Server.Production {
		r.HandleFunc("/", handleNoContent)
	} else {
		r.Handle("/", &defaultHandler{config, layerMap, auth})
		// r.HandleFunc("/documentation", defaultHandler)
	}
	r.Handle(config.Server.ContextRoot+"/{layer}/{z}/{x}/{y}", &tileHandler{defaultHandler{config, layerMap, auth}})

	var rootHandler http.Handler

	rootHandler = &r

	if config.Server.Gzip {
		rootHandler = handlers.CompressHandler(rootHandler)
	}

	if config.Logging.AccessLog {
		var out io.Writer
		if config.Logging.Path == "STDOUT" {
			out = os.Stdout
		} else {
			panic("TODO: access log in files")
		}
		//TODO: support file
		rootHandler = handlers.LoggingHandler(out, rootHandler)
	}

	http.ListenAndServe(config.Server.BindHost+":"+strconv.Itoa(config.Server.Port), rootHandler)
}
