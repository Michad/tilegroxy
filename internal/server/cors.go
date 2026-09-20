// Copyright 2026 Michael Davis
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

package server

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/pkg/config"
)

const corsWildcard = "*"

func ValidateCORS(cfg config.CORSConfig, errorMessages config.ErrorMessages) error {
	if !cfg.Enabled {
		return nil
	}

	if !cfg.AllowCredentials {
		return nil
	}

	if cfg.WildcardOrigin {
		return fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "server.cors.wildcardorigin", "server.cors.allowcredentials")
	}

	for _, h := range cfg.Headers {
		if h == corsWildcard {
			return fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "server.cors.headers=*", "server.cors.allowcredentials")
		}
	}

	for _, h := range cfg.ExposedHeaders {
		if h == corsWildcard {
			return fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "server.cors.exposedheaders=*", "server.cors.allowcredentials")
		}
	}

	return nil
}

// Confirms if an origin should be allowed. Returns (origin to use in header, allowed)
func checkAllowedOrigin(cfg config.CORSConfig, origin string) (string, bool) {
	if origin == "" {
		return "", false
	}

	if cfg.WildcardOrigin {
		return corsWildcard, true
	}

	for _, pattern := range cfg.Origins {
		if originMatchesPattern(pattern, origin) {
			return origin, true
		}
	}

	return "", false
}

// Compares origin pattern against origin. Scheme and port are optional in the pattern, wildcards allowed
func originMatchesPattern(pattern, origin string) bool {
	patternScheme, patternRest := splitScheme(pattern)
	originScheme, originRest := splitScheme(origin)

	if patternScheme != "" && !strings.EqualFold(patternScheme, originScheme) {
		return false
	}

	patternHost, patternPort := splitPort(patternRest)
	originHost, originPort := splitPort(originRest)

	if patternPort != "" && patternPort != originPort {
		return false
	}

	return hostMatchesPattern(patternHost, originHost)
}

func splitScheme(s string) (string, string) {
	if i := strings.Index(s, "://"); i >= 0 {
		return s[:i], s[i+3:]
	}

	return "", s
}

func splitPort(s string) (string, string) {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		// Assuming this is because there's no explicit port - just strip ipv6 brackets
		return strings.Trim(s, "[]"), ""
	}

	return host, port
}

// Evaluate that a host (no scheme/port) matches a pattern. Supporting * and **
func hostMatchesPattern(pattern, host string) bool {
	if pattern == "" {
		return false
	}

	return hostMatchRecursive(strings.Split(strings.ToLower(pattern), "."), strings.Split(strings.ToLower(host), "."))
}

func hostMatchRecursive(pattern, host []string) bool {
	if len(pattern) == 0 {
		return len(host) == 0
	}

	if pattern[0] == "**" {
		// Greedy but bounded: try consuming one label at a time until the rest of the pattern fits
		for i := 1; i <= len(host); i++ {
			if hostMatchRecursive(pattern[1:], host[i:]) {
				return true
			}
		}

		return false
	}

	if len(host) == 0 {
		return false
	}

	if pattern[0] != corsWildcard && pattern[0] != host[0] {
		return false
	}

	return hostMatchRecursive(pattern[1:], host[1:])
}

func writeCORSHeaders(cfg config.CORSConfig, w http.ResponseWriter, req *http.Request) {
	if !cfg.Enabled {
		return
	}

	preflight := req.Method == http.MethodOptions && req.Header.Get("Access-Control-Request-Method") != ""

	addVary(w, "Origin")

	if preflight {
		addVary(w, "Access-Control-Request-Method")
		addVary(w, "Access-Control-Request-Headers")
	}

	// Cleared before anything is written so a stale Server.Headers entry never survives
	clearCORSHeaders(w)

	origin, ok := checkAllowedOrigin(cfg, req.Header.Get("Origin"))
	if !ok {
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", origin)

	if cfg.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}

	if len(cfg.ExposedHeaders) > 0 {
		w.Header().Set("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ", "))
	}

	if !preflight {
		return
	}

	if len(cfg.Methods) > 0 {
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.Methods, ", "))
	}

	writeAllowHeaders(cfg, w, req)

	if cfg.MaxAge > 0 {
		w.Header().Set("Access-Control-Max-Age", strconv.FormatUint(uint64(cfg.MaxAge), 10))
	}
}

func writeAllowHeaders(cfg config.CORSConfig, w http.ResponseWriter, req *http.Request) {
	for _, h := range cfg.Headers {
		if h == corsWildcard {
			if requested := req.Header.Get("Access-Control-Request-Headers"); requested != "" {
				w.Header().Set("Access-Control-Allow-Headers", requested)
			}

			return
		}
	}

	if len(cfg.Headers) > 0 {
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.Headers, ", "))
	}
}

// The headers CORS owns once enabled
var corsOwnedHeaders = []string{
	"Access-Control-Allow-Origin",
	"Access-Control-Allow-Credentials",
	"Access-Control-Expose-Headers",
	"Access-Control-Allow-Methods",
	"Access-Control-Allow-Headers",
	"Access-Control-Max-Age",
}

func clearCORSHeaders(w http.ResponseWriter) {
	for _, h := range corsOwnedHeaders {
		w.Header().Del(h)
	}
}

func addVary(w http.ResponseWriter, name string) {
	for _, existing := range w.Header().Values("Vary") {
		for _, v := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(v), name) {
				return
			}
		}
	}

	w.Header().Add("Vary", name)
}

type corsHandler struct {
	inner http.Handler
	cfg   config.CORSConfig
}

func (h corsHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.inner.ServeHTTP(&corsResponseWriter{ResponseWriter: w, cfg: h.cfg, req: req}, req)
}

type corsResponseWriter struct {
	http.ResponseWriter
	cfg     config.CORSConfig
	req     *http.Request
	written bool
}

func (w *corsResponseWriter) WriteHeader(status int) {
	w.applyCORS()
	w.ResponseWriter.WriteHeader(status)
}

func (w *corsResponseWriter) Write(b []byte) (int, error) {
	w.applyCORS()
	return w.ResponseWriter.Write(b)
}

func (w *corsResponseWriter) applyCORS() {
	if w.written {
		return
	}

	w.written = true

	writeCORSHeaders(w.cfg, w.ResponseWriter, w.req)
}

// Preserved so the gzip and timeout layers can still detect flushing support through this wrapper
func (w *corsResponseWriter) Flush() {
	w.applyCORS()

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *corsResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
