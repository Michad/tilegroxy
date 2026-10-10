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

package authentications

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/internal/util"
	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/authentication"
	"github.com/antchfx/jsonquery"
	"github.com/antchfx/xmlquery"
	"github.com/antchfx/xpath"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const passthroughDefaultCacheTTL = 600

var passthroughMethods = []string{http.MethodGet, http.MethodHead, http.MethodPost}

type PassthroughConfig struct {
	URL         string               // The endpoint to call. Required
	Method      string               // One of GET, HEAD, or POST. Defaults to GET
	Headers     []string             // Incoming headers to forward
	Cookies     []string             // Incoming cookies to forward
	QueryParams []string             // Incoming query parameters to forward
	UserID      string               // XPath to the user ID in the JSON or XML response body
	TenantID    string               // XPath to the tenant ID in the JSON or XML response body
	CacheSize   int                  // How many results to keep in memory. Zero or negative disables the cache, which is the default
	CacheTTL    uint32               // Seconds a result is reused. Defaults to 600
	Client      *config.ClientConfig // Overrides parameters of the top-level client
}

type passthroughResult struct {
	pass     bool
	userID   string
	tenantID string
}

type passthroughForward struct {
	headers http.Header
	cookies []*http.Cookie
	query   url.Values
	key     string
}

type Passthrough struct {
	PassthroughConfig
	endpoint     *url.URL
	clientConfig config.ClientConfig
	httpClient   *http.Client
	userPath     *xpath.Expr
	tenantPath   *xpath.Expr
	cache        *lookupCache[passthroughResult]
}

func init() {
	authentication.RegisterAuthentication(PassthroughRegistration{})
}

type PassthroughRegistration struct {
}

func (s PassthroughRegistration) InitializeConfig() any {
	return PassthroughConfig{}
}

func (s PassthroughRegistration) Name() string {
	return "passthrough"
}

func (s PassthroughRegistration) Initialize(cfgAny any, deps authentication.AuthenticationDeps) (authentication.Authentication, error) {
	cfg := cfgAny.(PassthroughConfig)
	errorMessages := deps.ErrorMessages

	if cfg.URL == "" {
		return nil, fmt.Errorf(errorMessages.ParamRequired, "authentication.url")
	}

	endpoint, err := url.Parse(cfg.URL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "authentication.url", util.RedactURLForLog(cfg.URL))
	}

	if cfg.Method == "" {
		cfg.Method = http.MethodGet
	}
	cfg.Method = strings.ToUpper(cfg.Method)
	if !slices.Contains(passthroughMethods, cfg.Method) {
		return nil, fmt.Errorf(errorMessages.EnumError, "authentication.method", cfg.Method, passthroughMethods)
	}

	if len(cfg.Headers) == 0 && len(cfg.Cookies) == 0 && len(cfg.QueryParams) == 0 {
		return nil, fmt.Errorf(errorMessages.OneOfRequired, []string{"authentication.headers", "authentication.cookies", "authentication.queryparams"})
	}

	headers := make([]string, len(cfg.Headers))
	for i, h := range cfg.Headers {
		headers[i] = http.CanonicalHeaderKey(h)
	}
	cfg.Headers = headers

	for _, name := range cfg.QueryParams {
		if endpoint.Query().Has(name) {
			return nil, fmt.Errorf(errorMessages.InvalidParam, "authentication.queryparams", name)
		}
	}

	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = passthroughDefaultCacheTTL
	}

	p := Passthrough{PassthroughConfig: cfg, endpoint: endpoint}

	if p.userPath, err = parsePassthroughPath(cfg.UserID, "authentication.userid", errorMessages); err != nil {
		return nil, err
	}
	if p.tenantPath, err = parsePassthroughPath(cfg.TenantID, "authentication.tenantid", errorMessages); err != nil {
		return nil, err
	}

	// A HEAD response has no body to read IDs from
	if cfg.Method == http.MethodHead && p.readsBody() {
		param := util.Ternary(p.userPath != nil, "authentication.userid", "authentication.tenantid")
		return nil, fmt.Errorf(errorMessages.ParamsMutuallyExclusive, "authentication.method: HEAD", param)
	}

	p.clientConfig = deps.ClientConfig
	if cfg.Client != nil {
		p.clientConfig = *cfg.Client
		configload.MergeClientDefaults(&p.clientConfig, deps.ClientConfig)
	}

	// The default content types are all imagery, which would fail every request at runtime
	if p.readsBody() && !slices.ContainsFunc(p.clientConfig.ContentTypes, isPassthroughBodyType) {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "authentication.client.contenttypes", p.clientConfig.ContentTypes)
	}

	p.httpClient = &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   time.Duration(min(p.clientConfig.Timeout, math.MaxInt32)) * time.Second,
		// A login page reached by redirect would otherwise be mistaken for a successful check
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	if p.cache, err = newLookupCache[passthroughResult](cfg.CacheSize, cfg.CacheTTL); err != nil {
		return nil, err
	}

	return &p, nil
}

func parsePassthroughPath(expr string, param string, errorMessages config.ErrorMessages) (*xpath.Expr, error) {
	if expr == "" {
		return nil, nil
	}

	path, err := xpath.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf(errorMessages.InvalidParam, param, expr)
	}

	return path, nil
}

func isPassthroughBodyType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	_, subtype, _ := strings.Cut(mediaType, "/")
	for _, format := range []string{"json", "xml"} {
		if subtype == format || strings.HasSuffix(subtype, "+"+format) {
			return true
		}
	}

	return false
}

func (p Passthrough) readsBody() bool {
	return p.userPath != nil || p.tenantPath != nil
}

func (p Passthrough) CheckAuthentication(ctx context.Context, req *http.Request) bool {
	fwd, ok := p.collect(req)
	if !ok {
		slog.Log(ctx, config.LevelTrace, "Request lacked any values to forward for passthrough authentication")
		return false
	}

	result, err := p.cache.get(ctx, fwd.key, func() (passthroughResult, error) { return p.call(ctx, fwd) })
	if err != nil {
		slog.ErrorContext(ctx, "Passthrough authentication request failed", "error", err)
		return false
	}
	if !result.pass {
		return false
	}

	if ctxUserID, ok := pkg.UserIDFromContext(ctx); ok {
		*ctxUserID = result.userID
	}
	if ctxTenantID, ok := pkg.TenantIDFromContext(ctx); ok {
		*ctxTenantID = result.tenantID
	}

	return true
}

func (p Passthrough) Close(_ context.Context) error {
	p.cache.close()

	return nil
}

func (p Passthrough) collect(req *http.Request) (passthroughForward, bool) {
	fwd := passthroughForward{headers: http.Header{}, query: url.Values{}}
	// Missing values are kept as empty entries so values can't shift into another name's slot
	keyParts := make([][]string, 0, len(p.Headers)+len(p.Cookies)+len(p.QueryParams))
	found := false

	for _, name := range p.Headers {
		vals := req.Header[name]
		if len(vals) > 0 {
			fwd.headers[name] = slices.Clone(vals)
			found = true
		}
		keyParts = append(keyParts, vals)
	}

	for _, name := range p.Cookies {
		cookies := req.CookiesNamed(name)
		vals := make([]string, 0, len(cookies))
		for _, c := range cookies {
			fwd.cookies = append(fwd.cookies, c)
			vals = append(vals, c.Value)
			found = true
		}
		keyParts = append(keyParts, vals)
	}

	query := req.URL.Query()
	for _, name := range p.QueryParams {
		vals := query[name]
		if len(vals) > 0 {
			fwd.query[name] = vals
			found = true
		}
		keyParts = append(keyParts, vals)
	}

	if !found {
		return fwd, false
	}

	// Hashed to bound the memory a hostile request can make the cache hold
	encoded, _ := json.Marshal(keyParts)
	sum := sha256.Sum256(encoded)
	fwd.key = hex.EncodeToString(sum[:])

	return fwd, true
}

func (p Passthrough) call(ctx context.Context, fwd passthroughForward) (passthroughResult, error) {
	target := *p.endpoint
	if len(fwd.query) > 0 {
		target.RawQuery = strings.TrimPrefix(target.RawQuery+"&"+fwd.query.Encode(), "&")
	}

	outReq, err := http.NewRequestWithContext(ctx, p.Method, target.String(), nil)
	if err != nil {
		return passthroughResult{}, err
	}

	for h, v := range fwd.headers {
		outReq.Header[h] = v
	}
	// Operator values win over anything forwarded from the user
	outReq.Header.Set("User-Agent", p.clientConfig.UserAgent)
	for h, v := range p.clientConfig.Headers {
		outReq.Header.Set(h, v)
	}
	for _, c := range fwd.cookies {
		outReq.AddCookie(c)
	}

	resp, err := p.httpClient.Do(outReq)
	if err != nil {
		// Forwarded query values are user credentials that the generic redaction doesn't know about
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			urlErr.URL = p.URL
		}
		failure := util.DescribeClientError(err)
		slog.DebugContext(ctx, failure.Detail)
		if failure.Canceled {
			return passthroughResult{}, context.Canceled
		}
		return passthroughResult{}, pkg.RemoteConnectionError{Cause: failure.Cause, Timeout: failure.Timeout}
	}
	defer resp.Body.Close()

	slog.DebugContext(ctx, "Passthrough authentication response", "status", resp.StatusCode)

	if !slices.Contains(p.clientConfig.StatusCodes, resp.StatusCode) {
		return passthroughResult{}, nil
	}

	if !p.readsBody() {
		return passthroughResult{pass: true}, nil
	}

	body, err := p.readBody(resp)
	if err != nil {
		return passthroughResult{}, err
	}

	return p.extractIDs(ctx, body), nil
}

func (p Passthrough) readBody(resp *http.Response) ([]byte, error) {
	contentType := resp.Header.Get("Content-Type")
	if !slices.Contains(p.clientConfig.ContentTypes, contentType) {
		return nil, pkg.InvalidContentTypeError{ContentType: contentType}
	}

	if resp.ContentLength == -1 {
		if p.clientConfig.UnknownLength == nil || !*p.clientConfig.UnknownLength {
			return nil, pkg.InvalidContentLengthError{Length: -1}
		}
	} else if resp.ContentLength > int64(p.clientConfig.MaxLength) {
		return nil, pkg.InvalidContentLengthError{Length: int(resp.ContentLength)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(p.clientConfig.MaxLength)+1))
	if err != nil {
		return nil, pkg.RemoteServerError{StatusCode: resp.StatusCode}
	}

	if len(body) > p.clientConfig.MaxLength {
		return nil, pkg.InvalidContentLengthError{Length: len(body)}
	}

	return body, nil
}

func (p Passthrough) extractIDs(ctx context.Context, body []byte) passthroughResult {
	var nav xpath.NodeNavigator

	// JSON can't start with '<', so this is unambiguous regardless of the Content-Type
	if bytes.HasPrefix(bytes.TrimLeft(body, " \t\r\n\ufeff"), []byte("<")) {
		doc, err := xmlquery.Parse(bytes.NewReader(body))
		if err != nil {
			slog.WarnContext(ctx, "Passthrough authentication response was not valid XML", "error", err)
			return passthroughResult{}
		}
		nav = xmlquery.CreateXPathNavigator(doc)
	} else {
		doc, err := jsonquery.Parse(bytes.NewReader(body))
		if err != nil {
			slog.WarnContext(ctx, "Passthrough authentication response was not valid JSON", "error", err)
			return passthroughResult{}
		}
		nav = jsonquery.CreateXPathNavigator(doc)
	}

	userID, ok := selectPassthroughID(ctx, p.userPath, nav, "userId")
	if !ok {
		return passthroughResult{}
	}

	tenantID, ok := selectPassthroughID(ctx, p.tenantPath, nav, "tenantId")
	if !ok {
		return passthroughResult{}
	}

	return passthroughResult{pass: true, userID: userID, tenantID: tenantID}
}

func selectPassthroughID(ctx context.Context, path *xpath.Expr, nav xpath.NodeNavigator, param string) (string, bool) {
	if path == nil {
		return "", true
	}

	var val string
	var ok bool

	switch result := path.Evaluate(nav.Copy()).(type) {
	case string:
		val, ok = result, true
	case float64:
		val, ok = formatPassthroughNumber(result)
	case *xpath.NodeIterator:
		if !result.MoveNext() {
			return "", true
		}
		node := result.Current().Copy()
		if result.MoveNext() {
			slog.WarnContext(ctx, "Passthrough authentication XPath matched more than one value", "param", param)
			return "", false
		}
		val, ok = passthroughNodeValue(node)
	}

	if !ok {
		slog.WarnContext(ctx, "Passthrough authentication XPath matched a value that isn't a string or number", "param", param)
	}

	return val, ok
}

func formatPassthroughNumber(f float64) (string, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", false
	}

	return strconv.FormatFloat(f, 'f', -1, 64), true
}

func passthroughNodeValue(node xpath.NodeNavigator) (string, bool) {
	if jsonNode, ok := node.(*jsonquery.NodeNavigator); ok {
		switch v := jsonNode.GetValue().(type) {
		case string:
			return v, true
		case float64:
			return formatPassthroughNumber(v)
		default:
			return "", false
		}
	}

	switch node.NodeType() {
	case xpath.AttributeNode, xpath.TextNode:
		return strings.TrimSpace(node.Value()), true
	case xpath.ElementNode:
		child := node.Copy()
		for more := child.MoveToChild(); more; more = child.MoveToNext() {
			if child.NodeType() == xpath.ElementNode {
				return "", false
			}
		}
		return strings.TrimSpace(node.Value()), true
	case xpath.RootNode, xpath.CommentNode:
		return "", false
	default:
		return "", false
	}
}
