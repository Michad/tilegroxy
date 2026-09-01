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

//go:build e2e

package e2e

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	// How long a single request may take before it counts as a failure.
	requestTimeout = 30 * time.Second
	// How much of a body an assertion failure prints before it stops being useful.
	bodyExcerpt = 512
)

// Response is a fully read HTTP response. The body is consumed and closed on construction, which
// is what lets tests chain assertions without tracking Close calls.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte

	t *testing.T
}

func (i *Instance) Get(path string) *Response {
	i.t.Helper()

	return i.doGet(i.BaseURL() + path)
}

// GetWithHeader behaves like Get but sets a single request header, which is what auth schemes
// that read from headers (e.g. static key's "Authorization: Bearer ...") need to be exercised.
func (i *Instance) GetWithHeader(path, header, value string) *Response {
	i.t.Helper()

	return i.doGetWith(i.BaseURL()+path, nil, func(req *http.Request) {
		req.Header.Set(header, value)
	})
}

func (i *Instance) GetHealth() *Response {
	i.t.Helper()

	return i.doGet(i.HealthURL() + "/health")
}

// GetNoRedirect returns the first response rather than following it, so a test can assert on a
// redirect itself. The default handler answers unrouted paths with a 307 to the docs path, which a
// following client would otherwise report as the docs page's 200.
func (i *Instance) GetNoRedirect(path string) *Response {
	i.t.Helper()

	return i.doGetWith(i.BaseURL()+path, func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}, nil)
}

// doGet reports the child's captured output when a request fails, since a transport error usually
// means the server logged the real reason.
func (i *Instance) doGet(url string) *Response {
	i.t.Helper()

	return i.doGetWith(url, nil, nil)
}

func (i *Instance) doGetWith(url string, checkRedirect func(*http.Request, []*http.Request) error, mutate func(*http.Request)) *Response {
	i.t.Helper()

	client := &http.Client{Timeout: Scale(requestTimeout), CheckRedirect: checkRedirect}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		i.t.Fatalf("cannot build request for %v: %v", url, err)
	}

	if mutate != nil {
		mutate(req)
	}

	resp, err := client.Do(req)
	if err != nil {
		i.t.Fatalf("GET %v failed: %v\nOutput:\n%s", url, err, i.Output())
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			i.t.Fatalf("cannot close body for %v: %v", url, err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		i.t.Fatalf("cannot read body for %v: %v", url, err)
	}

	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: body, t: i.t}
}

func (r *Response) ExpectStatus(code int) *Response {
	r.t.Helper()

	if r.StatusCode != code {
		r.t.Errorf("expected status %v, got %v. Body: %s", code, r.StatusCode, truncate(r.Body))
	}

	return r
}

func (r *Response) ExpectHeader(name, value string) *Response {
	r.t.Helper()

	if got := r.Header.Get(name); got != value {
		r.t.Errorf("expected header %v to be %q, got %q", name, value, got)
	}

	return r
}

func (r *Response) ExpectBodyContains(substr string) *Response {
	r.t.Helper()

	if !strings.Contains(string(r.Body), substr) {
		r.t.Errorf("expected body to contain %q, got: %s", substr, truncate(r.Body))
	}

	return r
}

func truncate(b []byte) string {
	if len(b) > bodyExcerpt {
		return string(b[:bodyExcerpt]) + "..."
	}

	return string(b)
}
