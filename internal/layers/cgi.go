// Copyright 2024 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package layers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"slices"
	"strings"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

type CGIConfig struct {
	Exec           string              //The path to the CGI executable
	Args           []string            //Arguments to pass into the executable in standard "split on spaces" format
	Uri            string              //The URI (path + query) to pass into the CGI for the fake request - think mod_rewrite style invocation of the CGI
	Domain         string              //The host to pass into the CGI for the fake request. Defaults to localhost
	Headers        map[string][]string //Extra headers to pass into the CGI for the fake request
	Env            map[string]string   //Environment variables to supply to the CGI invocations. If the value is an empty string it passes along the value from the main tilegroxy invocation
	WorkingDir     string              //Working directory for the CGI invocation
	InvalidAsError bool                //If true, if the CGI response includes a content type that isn't in the Client's list of acceptable content types then it treats the response body as an error message
}

type CGI struct {
	CGIConfig
	handler      cgi.Handler
	clientConfig config.ClientConfig
}

type SLogWriter struct {
	ctx   *internal.RequestContext
	level slog.Level
}

// TODO: look into some ways to buffer and decrease num of calls to slog
func (w SLogWriter) Write(p []byte) (n int, err error) {
	slog.Log(w.ctx, w.level, string(p))

	return len(p), nil
}

type response struct {
	buff    io.Writer
	code    int
	headers map[string][]string
}

func (r *response) Flush() {
}

func (r *response) Header() http.Header {
	return r.headers
}

func (r *response) Write(p []byte) (n int, err error) {
	// fmt.Println(string(p))
	return r.buff.Write(p)
}

func (r *response) WriteHeader(code int) {
	r.code = code
}

func ConstructCGI(cfg CGIConfig, clientConfig config.ClientConfig, errorMessages config.ErrorMessages) (*CGI, error) {
	env := make([]string, 0)
	inheritEnv := make([]string, 0)

	if cfg.Exec == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "provider.cgi.exec")
	}

	if cfg.Uri == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "provider.cgi.uri")
	}

	if cfg.Domain == "" {
		cfg.Domain = "localhost"
	}

	if cfg.Env != nil {
		for k, v := range cfg.Env {
			if v == "" {
				inheritEnv = append(inheritEnv, k)
			} else {
				env = append(env, strings.ToUpper(k)+"="+v)
			}
		}
	}

	h := cgi.Handler{
		Path:       cfg.Exec,
		Env:        env,
		InheritEnv: inheritEnv,
		Args:       cfg.Args,
		Dir:        cfg.WorkingDir,
	}

	return &CGI{cfg, h, clientConfig}, nil
}

func (t CGI) PreAuth(ctx *internal.RequestContext, providerContext ProviderContext) (ProviderContext, error) {
	return ProviderContext{AuthBypass: true}, nil
}

func (t CGI) GenerateTile(ctx *internal.RequestContext, providerContext ProviderContext, tileRequest internal.TileRequest) (*internal.Image, error) {
	var err error

	h := t.handler

	h.Stderr = SLogWriter{ctx, slog.LevelError.Level()}
	h.Logger = log.New(h.Stderr, "", 0)

	uri := t.Uri
	if uri[0] != '/' {
		uri = "/" + uri
	}

	uri, err = replaceUrlPlaceholders(ctx, tileRequest, uri, false)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, fmt.Sprintf("Calling %v", uri))

	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+t.Domain+uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header = t.Headers

	var buf bytes.Buffer
	rw := response{&buf, 0, make(map[string][]string)}

	h.ServeHTTP(&rw, req)
	b := buf.Bytes()

	slog.DebugContext(ctx, fmt.Sprintf("CGI response - Status: %v Content: %v", rw.code, rw.headers["Content-Type"]))

	if !slices.Contains(t.clientConfig.StatusCodes, rw.code) {
		return nil, fmt.Errorf("cgi returned status code %v", rw.code)
	}

	if t.InvalidAsError && !slices.Contains(t.clientConfig.ContentTypes, rw.headers["Content-Type"][0]) {
		return nil, errors.New(string(b))
	}

	return &b, nil
}
