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
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/internal/util"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetTile is exported from pkg so a library consumer writing their own Go provider gets the same
// MaxLength/ContentTypes/StatusCodes enforcement as the built-in ones without reimplementing it.
func Test_GetTile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tiledata"))
	}))
	defer server.Close()

	clientConfig := config.ClientConfig{
		StatusCodes:  []int{http.StatusOK},
		ContentTypes: []string{"image/png"},
		MaxLength:    1024,
		Timeout:      5,
	}

	img, err := GetTile(context.Background(), clientConfig, server.URL, nil)

	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, []byte("tiledata"), img.Content)
	assert.Equal(t, "image/png", img.ContentType)
}

func Fuzz_EncodeDecodeImage(f *testing.F) {
	for z := 1; z < 100; z++ {
		b := make([]byte, rand.IntN(1000))

		for i := range b {
			b[i] = byte(rand.UintN(255))
		}

		c := util.RandomString() + "/" + util.RandomString()

		f.Add(b, c)
	}
	f.Fuzz(func(t *testing.T, b []byte, c string) {
		img1 := Image{Content: b, ContentType: c, CreatedAt: 1234567890}

		b, err := img1.Encode()
		require.NoError(t, err)
		img2, err := DecodeImage(b)
		require.NoError(t, err)

		assert.Equal(t, img1.ContentType, img2.ContentType)
		assert.Equal(t, img1.Content, img2.Content)
		assert.Equal(t, img1.CreatedAt, img2.CreatedAt)

		// Test backwards compatibility
		img3, err := DecodeImage(img1.Content)
		require.NoError(t, err)

		if len(img1.Content) == 0 {
			assert.Nil(t, img3)
		} else {
			assert.Equal(t, img3.Content, img2.Content)
			assert.Empty(t, img3.ContentType)
		}
	})
}

// A truncated or empty payload must read as a cache miss, not a successful empty tile.
func Test_DecodeImage_Empty(t *testing.T) {
	for _, b := range [][]byte{nil, {}} {
		img, err := DecodeImage(b)
		require.NoError(t, err)
		assert.Nil(t, img)
	}
}

// v1 payloads (Content + ContentType, no CreatedAt) must still decode after v2 was introduced.
func Test_DecodeImage_V1(t *testing.T) {
	b := bytes.Buffer{}
	e := gob.NewEncoder(&b)

	require.NoError(t, e.Encode("v1"))
	require.NoError(t, e.Encode([]byte("tiledata")))
	require.NoError(t, e.Encode("image/png"))

	img, err := DecodeImage(b.Bytes())
	require.NoError(t, err)
	require.NotNil(t, img)
	assert.Equal(t, []byte("tiledata"), img.Content)
	assert.Equal(t, "image/png", img.ContentType)
	assert.Zero(t, img.CreatedAt)
}

func Test_GetTile_ErrorsAreValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/type":
			w.Header().Set("Content-Type", "text/html")
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("too long"))
		}
	}))
	defer server.Close()

	clientConfig := config.ClientConfig{
		StatusCodes:  []int{http.StatusOK},
		ContentTypes: []string{"image/png"},
		MaxLength:    2,
		Timeout:      5,
	}

	_, err := GetTile(context.Background(), clientConfig, server.URL+"/status", nil)
	var remoteErr RemoteServerError
	require.ErrorAs(t, fmt.Errorf("wrapped: %w", err), &remoteErr)
	assert.Equal(t, http.StatusServiceUnavailable, remoteErr.StatusCode)

	_, err = GetTile(context.Background(), clientConfig, server.URL+"/type", nil)
	var typeErr InvalidContentTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "text/html", typeErr.ContentType)

	_, err = GetTile(context.Background(), clientConfig, server.URL+"/length", nil)
	var lengthErr InvalidContentLengthError
	require.ErrorAs(t, err, &lengthErr)
}
