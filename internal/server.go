package internal

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Michad/tilegroxy/internal/authentication"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/internal/layers"

	"github.com/gorilla/handlers"
)

func defaultHandler(w http.ResponseWriter, req *http.Request) {

	ctx := req.Context()
	fmt.Println("server: hello handler started")
	defer fmt.Println("server: hello handler ended")

	select {
	case <-time.After(10 * time.Second):
		fmt.Fprintf(w, "hello\n")
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

func handleTile(w http.ResponseWriter, req *http.Request) {

	ctx := req.Context()
	fmt.Println("server: hello handler started")
	defer fmt.Println("server: hello handler ended")

	select {
	case <-time.After(10 * time.Second):
		fmt.Fprintf(w, "hello\n")
	case <-ctx.Done():

		err := ctx.Err()
		fmt.Println("server:", err)
		internalError := http.StatusInternalServerError
		http.Error(w, err.Error(), internalError)
	}
}

func ListenAndServe(config config.Config, layers []*layers.Layer, auth *authentication.Authentication, ) {
	r := http.NewServeMux()

	r.HandleFunc(config.Server.ContextRoot+"/tile/{z}/{x}/{y}", handleTile)
	if config.Server.Production {
		r.HandleFunc("/", handleNoContent)
	} else {
		r.HandleFunc("/", defaultHandler)
		r.HandleFunc("/documentation", defaultHandler)
	}

	var rootHandler http.Handler

	rootHandler = r

	if config.Server.Gzip {
		rootHandler = handlers.CompressHandler(rootHandler)
	}

	if config.Logging.AccessLog {
		var out io.Writer
		if(config.Logging.Path == "STDOUT") {
			out = os.Stdout
		} else {
			panic("TODO: access log in files")
		}
		//TODO: support file
		rootHandler = handlers.LoggingHandler(out, rootHandler)
	}

	http.ListenAndServe(config.Server.BindHost+":"+strconv.Itoa(config.Server.Port), rootHandler)
}
