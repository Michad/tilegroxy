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

package pmtiles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Source interface {
	ReadRange(ctx context.Context, offset, length uint64) ([]byte, error)
	Close() error
}

var errRangeTooLarge = errors.New("pmtiles: requested range is too large")

// Capping at MaxInt32 keeps allocations bounded and the int conversions below safe
func checkRange(offset, length uint64) error {
	if offset > math.MaxInt64-math.MaxInt32 || length > math.MaxInt32 {
		return errRangeTooLarge
	}

	return nil
}

type FileSource struct {
	file *os.File
}

func OpenFile(path string) (*FileSource, error) {
	f, err := os.Open(path) // #nosec G304 -- path comes from operator configuration
	if err != nil {
		return nil, err
	}

	return &FileSource{file: f}, nil
}

func (s *FileSource) ReadRange(_ context.Context, offset, length uint64) ([]byte, error) {
	if err := checkRange(offset, length); err != nil {
		return nil, err
	}

	buf := make([]byte, length)
	n, err := s.file.ReadAt(buf, int64(offset)) // #nosec G115 -- range checked above
	if errors.Is(err, io.EOF) {
		return buf[:n], nil
	}

	return buf[:n], err
}

func (s *FileSource) Close() error {
	return s.file.Close()
}

var errRangeIgnored = errors.New("pmtiles: server ignored the range request and returned the whole archive")

type HTTPSource struct {
	url       string
	userAgent string
	headers   map[string]string
	client    *http.Client
}

func NewHTTPSource(url string, clientConfig config.ClientConfig) *HTTPSource {

	timeout := min(clientConfig.Timeout, math.MaxInt32)

	return &HTTPSource{
		url:       url,
		userAgent: clientConfig.UserAgent,
		headers:   clientConfig.Headers,
		client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   time.Duration(timeout) * time.Second, // #nosec G115 -- timeout clamped to MaxInt32 above
		},
	}
}

func (s *HTTPSource) ReadRange(ctx context.Context, offset, length uint64) ([]byte, error) {
	if err := checkRange(offset, length); err != nil {
		return nil, err
	}
	if length == 0 {
		return []byte{}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", s.userAgent)
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		return nil, errRangeIgnored
	default:
		return nil, pkg.RemoteServerError{StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(length)+1)) // #nosec G115 -- range checked above
	if err != nil {
		return nil, err
	}
	if uint64(len(body)) > length {
		return nil, fmt.Errorf("pmtiles: server returned more than the %d bytes requested", length)
	}

	return body, nil
}

func (s *HTTPSource) Close() error {
	s.client.CloseIdleConnections()
	return nil
}
