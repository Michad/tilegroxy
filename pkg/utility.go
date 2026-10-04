// Copyright 2024 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package pkg

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/Michad/tilegroxy/internal/static"
	"github.com/Michad/tilegroxy/internal/util"
	"github.com/Michad/tilegroxy/pkg/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// The main result type for tiles, usually raster or vector imagery but any content type is allowed
type Image struct {
	// Always an encoded file format, never raw RGB pixel values
	Content []byte
	// Forces the response Content-Type. Empty lets Go auto-detect. Should be a valid mime if set
	ContentType string
	// Prevents caching this result
	ForceSkipCache bool
	// Epoch seconds. Zero if unknown, e.g. decoded from a v1 payload
	CreatedAt int64
}

func (i *Image) SetCreatedNow() {
	i.CreatedAt = time.Now().Unix()
}

// Combines content and content type into one byte array for caches
func (i *Image) Encode() ([]byte, error) {
	b := bytes.Buffer{}
	e := gob.NewEncoder(&b)

	err := e.Encode("v2")

	if err != nil {
		return nil, err
	}

	err = e.Encode(i.Content)

	if err != nil {
		return nil, err
	}

	err = e.Encode(i.ContentType)

	if err != nil {
		return nil, err
	}

	if i.CreatedAt == 0 {
		i.SetCreatedNow()
	}

	err = e.Encode(i.CreatedAt)

	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// Reverses Image.Encode
func DecodeImage(b []byte) (*Image, error) {

	// An empty payload is corrupt
	if len(b) == 0 {
		return nil, nil
	}

	// Versioned separately from tilegroxy. v0 is Content. v1 adds Version and Content Type. v2 adds Created At
	i := Image{Content: []byte{}}
	e := gob.NewDecoder(bytes.NewBuffer(b))
	var version string

	err := e.Decode(&version)

	if err != nil || version != "v1" && version != "v2" && len(version) > 0 && version[0] != 'v' {
		// Raw imagery predating this encoding scheme
		return &Image{Content: b}, nil
	}

	if version != "v1" && version != "v2" {
		tgVersion, _, _ := static.GetVersionInformation()
		// Most likely written by a newer tilegroxy using e.g. a v3 schema
		return nil, fmt.Errorf("invalid binary cache encoding %v! You are likely running an old version of tilegroxy. Tilegroxy %v only supports through binary cache encoding v2", version, tgVersion)
	}

	err = e.Decode(&i.Content)

	if err != nil {
		return nil, err
	}

	err = e.Decode(&i.ContentType)

	if err != nil {
		return nil, err
	}

	if version == "v2" {
		err = e.Decode(&i.CreatedAt)

		if err != nil {
			return nil, err
		}
	}

	return &i, nil
}

// Applies the standard Client config: headers, timeout, allowlists, content-type rewriting and length limits. Custom Go providers should use it too
func GetTile(ctx context.Context, clientConfig config.ClientConfig, url string, authHeaders map[string]string) (*Image, error) {
	slog.DebugContext(ctx, fmt.Sprintf("Calling url %v\n", util.RedactURLForLog(url)))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", clientConfig.UserAgent)

	for h, v := range clientConfig.Headers {
		req.Header.Set(h, v)
	}

	for h, v := range authHeaders {
		req.Header.Set(h, v)
	}

	timeout := min(clientConfig.Timeout, math.MaxInt32)

	transport := otelhttp.NewTransport(http.DefaultTransport, otelhttp.WithMessageEvents(otelhttp.ReadEvents))
	client := http.Client{Transport: transport, Timeout: time.Duration(timeout) * time.Second}

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, fmt.Sprintf("Response status: %v", resp.StatusCode))

	if !slices.Contains(clientConfig.StatusCodes, resp.StatusCode) {
		return nil, RemoteServerError{StatusCode: resp.StatusCode}
	}

	contentType := resp.Header.Get("Content-Type")

	if !slices.Contains(clientConfig.ContentTypes, contentType) {
		return nil, InvalidContentTypeError{ContentType: contentType}
	}

	if clientConfig.RewriteContentTypes != nil {
		newContentType, ok := clientConfig.RewriteContentTypes[contentType]

		if ok {
			contentType = newContentType
		}
	}

	if resp.ContentLength == -1 {
		if clientConfig.UnknownLength == nil || !*clientConfig.UnknownLength {
			return nil, InvalidContentLengthError{Length: -1}
		}
	} else {
		if resp.ContentLength > int64(clientConfig.MaxLength) {
			return nil, InvalidContentLengthError{Length: int(resp.ContentLength)}
		}
	}

	img, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, RemoteServerError{StatusCode: resp.StatusCode}
	}

	if len(img) > clientConfig.MaxLength {
		return nil, InvalidContentLengthError{Length: len(img)}
	}

	return &Image{Content: img, ContentType: contentType}, nil
}
