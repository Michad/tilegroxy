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
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"strings"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
)

type CGIConfig struct {
	Exec        string
	Args        []string
	Uri         string
	Headers     map[string]string
	Env         map[string]string
	WorkingDir  string
	TextAsError bool
}

type CGI struct {
	CGIConfig
	handler cgi.Handler
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
	buff io.Writer
	code int
}

func (r *response) Flush() {
}

func (r *response) Header() http.Header {
	return make(http.Header)
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

	if cfg.Env != nil {
		for k, v := range cfg.Env {
			if v == "" {
				inheritEnv = append(inheritEnv, k)
			} else {
				env = append(env, strings.ToUpper(k)+"="+v)
			}
		}
	}

	log.Println(env)

	h := cgi.Handler{
		Path:       cfg.Exec,
		Env:        env,
		InheritEnv: inheritEnv,
		Args:       cfg.Args,
		Dir:        cfg.WorkingDir,
	}

	return &CGI{cfg, h}, nil
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

	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost"+uri, nil)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	rw := response{&buf, 0}

	h.ServeHTTP(&rw, req)
	b := buf.Bytes()
	// fmt.Printf("Final output %v", b)
	fmt.Printf("Status: %v \n", rw.code)
	// if rw.code > 300 {
	// 	return nil, fmt.Errorf("status code %v", rw.code)
	// }

	// if t.TextAsError {
	// 	isText := true
	// 	for _, c := range b {
	// 		if c > unicode.MaxASCII {
	// 			isText = false
	// 		}
	// 	}
	// 	if isText {
	// 		return nil, errors.New(string(b))
	// 	}
	// }

	return &b, nil
}
