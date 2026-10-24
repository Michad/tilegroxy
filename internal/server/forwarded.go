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
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal/ratelimit"
	"github.com/Michad/tilegroxy/pkg/config"
)

const xForwardedFor = "X-Forwarded-For"

type forwardedKey struct{}

// The original request as reported by a trusted proxy. Empty fields weren't reported
type forwardedRequest struct {
	proto  string
	host   string
	port   string
	prefix string
}

func forwardedFromContext(ctx context.Context) (forwardedRequest, bool) {
	f, ok := ctx.Value(forwardedKey{}).(forwardedRequest)
	return f, ok
}

func parseTrustedProxies(cfg config.ForwardedConfig, messages config.ErrorMessages) ([]netip.Prefix, error) {
	trusted := make([]netip.Prefix, 0, len(cfg.Trusted))

	for _, raw := range cfg.Trusted {
		prefix, err := ratelimit.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf(messages.InvalidParam, "server.forwarded.trusted", raw)
		}

		trusted = append(trusted, prefix)
	}

	return trusted, nil
}

// Outermost so every log, including the access log, sees the client IP
type forwardedHandler struct {
	http.Handler
	trusted []netip.Prefix
	headers config.ForwardedHeadersConfig
}

func (h forwardedHandler) isTrusted(addr netip.Addr) bool {
	for _, p := range h.trusted {
		if p.Contains(addr) {
			return true
		}
	}

	return false
}

func (h forwardedHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	peer, ok := parseAddr(req.RemoteAddr)
	if !ok || !h.isTrusted(peer) {
		h.Handler.ServeHTTP(w, req)
		return
	}

	if client, ok := h.clientIP(req); ok {
		req.RemoteAddr = client.String()
	}

	fwd := forwardedRequest{
		proto:  strings.ToLower(firstValue(req, h.headers.Proto)),
		host:   firstValue(req, h.headers.Host),
		port:   firstValue(req, h.headers.Port),
		prefix: strings.TrimSuffix(firstValue(req, h.headers.Prefix), "/"),
	}

	if fwd.proto != "http" && fwd.proto != "https" {
		fwd.proto = ""
	}

	if strings.ContainsAny(fwd.host, "/\\@ ") {
		fwd.host = ""
	}

	fwd.port = sanitizePort(fwd.port)

	if !validPrefix(fwd.prefix) {
		fwd.prefix = ""
	}

	h.Handler.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), forwardedKey{}, fwd)))
}

// X-Forwarded-For is a chain, so the right-most hop that isn't a trusted proxy is the client
func (h forwardedHandler) clientIP(req *http.Request) (netip.Addr, bool) {
	if h.headers.For == "" {
		return netip.Addr{}, false
	}

	var hops []string
	for _, v := range req.Header.Values(h.headers.For) {
		hops = append(hops, strings.Split(v, ",")...)
	}

	if len(hops) == 0 {
		return netip.Addr{}, false
	}

	if !strings.EqualFold(h.headers.For, xForwardedFor) {
		if len(hops) != 1 {
			return netip.Addr{}, false
		}

		return parseAddr(strings.TrimSpace(hops[0]))
	}

	var addr netip.Addr

	for i := len(hops) - 1; i >= 0; i-- {
		hop, ok := parseAddr(strings.TrimSpace(hops[i]))
		if !ok {
			return netip.Addr{}, false
		}

		addr = hop

		if !h.isTrusted(hop) {
			break
		}
	}

	return addr, true
}

// Accepts an address with or without a port
func parseAddr(raw string) (netip.Addr, bool) {
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}

	addr, err := netip.ParseAddr(strings.Trim(raw, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}

	return addr.Unmap().WithZone(""), true
}

// Proxies chaining onto an existing header append to it, so the first is the original
func firstValue(req *http.Request, header string) string {
	if header == "" {
		return ""
	}

	v, _, _ := strings.Cut(req.Header.Get(header), ",")

	return strings.TrimSpace(v)
}

func sanitizePort(raw string) string {
	port, err := strconv.ParseUint(raw, 10, 16)
	if err != nil || port == 0 {
		return ""
	}

	return strconv.FormatUint(port, 10)
}

// Anything but a plain path could alter the host or inject into URLs built from it
func validPrefix(prefix string) bool {
	if prefix == "" {
		return true
	}

	if prefix[0] != '/' {
		return false
	}

	for _, c := range prefix {
		isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlnum && !strings.ContainsRune("/-._~%!$&'()*+,;=:@", c) {
			return false
		}
	}

	return true
}
