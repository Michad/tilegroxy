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

package authentications

import (
	"net/http"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//note: JWTs here will expire in the year 2065. They will need to be updated on the off-chance this is still used 40 years from now

func TestFailMissingArgs(t *testing.T) {
	jwtConfig := JWTConfig{}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.Error(t, err)
	assert.Nil(t, jwt)
}
func TestFailMissingKey(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm: "HS256",
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.Error(t, err)
	assert.Nil(t, jwt)
}
func TestFailMissingAlg(t *testing.T) {
	jwtConfig := JWTConfig{
		Key: "hunter2",
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.Error(t, err)
	assert.Nil(t, jwt)
}

func TestGoodJwts(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm:     "HS256",
		Key:           "hunter2",
		MaxExpiration: 4294967295, // 136 years from now
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjMwMDAwMDAwMDB9.npKpCaeyhdn-CsbEc_AuPz3Nkmpeh6K73SYCaBMqWoE"} // Valid JWT with same key with expiration in the distant future
	assert.True(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))
}

func TestBadJwts(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm: "HS256",
		Key:       "hunter2",
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	req.Header["Authorization"] = []string{"unparseable"}
	assert.False(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.TsbkW6Baw6npF0SUva-SdB9gZ9MLtLFUMu3BtUnspzk"} // Valid JWT but with a different key
	assert.False(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.sCuKLbsVsWuzV45ZtOEslD0WHPyPYa4gkEBZNP084os"} // Valid JWT with same key but no expiration
	assert.False(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjMwMDAwMDAwMDB9.npKpCaeyhdn-CsbEc_AuPz3Nkmpeh6K73SYCaBMqWoE"} // Valid JWT with same key with expiration in the distant future
	assert.False(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))
}

func TestGoodJwtClaims(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm:        "HS256",
		Key:              "hunter2",
		MaxExpiration:    4294967295, // 136 years from now
		ExpectedAudience: "audience",
		ExpectedSubject:  "subject",
		ExpectedIssuer:   "issuer",
		ExpectedScope:    "tile",
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJpc3N1ZXIiLCJzY29wZSI6InNvbWV0aGluZyB0aWxlIG90aGVyIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjQyOTQ5NjcyOTV9.6jOBwjsvFcJXGkaleXB-75F6J3CjaQYuRELJPfvOfQE"} // Valid JWT with all claims
	assert.True(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))
}

func TestGoodJwtClaimsWithCache(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm:        "HS256",
		Key:              "hunter2",
		MaxExpiration:    4294967295, // 136 years from now
		ExpectedAudience: "audience",
		ExpectedSubject:  "subject",
		ExpectedIssuer:   "issuer",
		ExpectedScope:    "tile",
		CacheSize:        100,
	}
	jwtAny, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})
	jwt := jwtAny.(*JWT)

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJpc3N1ZXIiLCJzY29wZSI6InNvbWV0aGluZyB0aWxlIG90aGVyIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjQyOTQ5NjcyOTV9.6jOBwjsvFcJXGkaleXB-75F6J3CjaQYuRELJPfvOfQE"} // Valid JWT with all claims
	assert.True(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))
	assert.True(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))

	assert.Equal(t, 1, jwt.Cache.Size())
	date, ok := jwt.Cache.Get(req.Header["Authorization"][0])
	assert.True(t, ok)
	assert.Equal(t, int64(4294967295), date.Time.Unix())
}

func TestGoodJwtScopeLimit(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm:     "HS256",
		Key:           "hunter2",
		MaxExpiration: 4294967295, // 136 years from now
		LayerScope:    true,
		ScopePrefix:   "tile/",
		UserID:        "name",
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	ctx := pkg.BackgroundContext()

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJpc3N1ZXIiLCJzY29wZSI6InRpbGUvdGVzdCIsIm5hbWUiOiJKb2huIERvZSIsImlhdCI6MTUxNjIzOTAyMiwiZXhwIjo0Mjk0OTY3Mjk1fQ.j_-4ERnaVdkscbfjMKavieAtVH7GhZIBr5kwnKNHEAI"} // Valid JWT with scope=tile/test
	assert.True(t, jwt.CheckAuthentication(ctx, req))

	ctxLimitLayers, _ := pkg.LimitLayersFromContext(ctx)
	ctxAllowedLayers, _ := pkg.AllowedLayersFromContext(ctx)
	ctxUserID, _ := pkg.UserIDFromContext(ctx)
	assert.True(t, *ctxLimitLayers)

	if assert.Len(t, *ctxAllowedLayers, 1) {
		assert.Equal(t, "test", (*ctxAllowedLayers)[0])
	}

	assert.Equal(t, "John Doe", *ctxUserID)
}

func TestBadJwtClaims(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm:        "HS256",
		Key:              "hunter2",
		MaxExpiration:    4294967295, // 136 years from now
		ExpectedAudience: "audience",
		ExpectedSubject:  "subject",
		ExpectedIssuer:   "issuer",
		ExpectedScope:    "tile",
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYmFkIiwiaXNzIjoiaXNzdWVyIiwic2NvcGUiOiJzb21ldGhpbmcgdGlsZSBvdGhlciIsIm5hbWUiOiJKb2huIERvZSIsImlhdCI6MTUxNjIzOTAyMiwiZXhwIjo0Mjk0OTY3Mjk1fQ.1_i6c0LLPoQWrEB-Y1wJiEiKoCAwGRc3wE0FoFelcKQ"} // Invalid aud
	assert.False(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJiYWQiLCJhdWQiOiJhdWRpZW5jZSIsImlzcyI6Imlzc3VlciIsInNjb3BlIjoic29tZXRoaW5nIHRpbGUgb3RoZXIiLCJuYW1lIjoiSm9obiBEb2UiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6NDI5NDk2NzI5NX0.TtVgpJfEVEjTe6Z8FIHCiSqVsKD00MHL7OBDuLh78hw"} // Invalid sub
	assert.False(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJub2JvZHkgd2lsbCBldmVyIHJlYWQgdGhpcyIsInNjb3BlIjoic29tZXRoaW5nIHRpbGUgb3RoZXIiLCJuYW1lIjoiSm9obiBEb2UiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6NDI5NDk2NzI5NX0.IeaRecjpT4pQm6AUpJUoCUQskyGkcGXXab-Bccc2q3I"} // Invalid iss
	assert.False(t, jwt.CheckAuthentication(pkg.BackgroundContext(), req))

	//TODO: when implemented
	// req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJpc3N1ZXIiLCJzY29wZSI6InNvbWV0aGluZyBvdGhlciIsIm5hbWUiOiJKb2huIERvZSIsImlhdCI6MTUxNjIzOTAyMiwiZXhwIjo0Mjk0OTY3Mjk1fQ.yt4Ga01Mn5wIUglH67gPr4NEt4g9AlwEFiTy8YNN-8g"} // Invalid scope
	// assert.False(t, jwt.Preauth(req))
}

func TestGoodJwtClaimsRS256(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm: "RS256",
		Key: `-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAzhZBQzvlt1KBBCJd6dWs
bIS1xNMw4h8ji4+QVVEept2A0RIL5PCTKWPAImtNRDQISzsSvyVA6Pk3/cOvCMLE
tLPQZaibrju8DhQtdK8+7lQk/PteGvwitElWvkIwpJ9hVxUA2CRAu3l6msK+S9V0
eS88UYkihL4XpzzJ1doJQLfB+I8DnHerHuI+qf32XlimLCDzMYj8hMl1RtLJP6YT
EROt3/1zYxCIfT46EcTXJgO7G5ilVWQdDL+ui0GIOVFmA5EqzdVaLhp5xhyffXHb
Y2M9nEYGngcPa8uZnbNsMipWsolP/vUF951GSHruY+oBQZ5RsGov9mpw8MZl+LNv
g2G+aONpHJEZ6iTsMYofj6VJu3O+arLtBu+VTayhjUxcD2h2cnEurV5W5tyCcgFm
BZUXaiwMlBDaFQJBEsb3v0XrJ7H4/CRIipgRf6o/+SIv1FlPkgtyXmLMuWpM7iYA
7OpTLXR4yinBxvWhsot9yhm9ruqs0ZCkOQ0rTn8C5JZwi+tWtQRvp1k8Jv7SS/Dc
0dnAAsRNy4f/DUFaZESdUOvnDTSXt/2VHejkrhc7DeXynTySKMU3t8UqXNHmHZuq
hBcM4yPXyRd2KGlrbwJ/m1VSNjCI6wRkyHvM8ZgYezOHBcvlHlPATPsPK9r+bN7k
vxNWUY5rv006ZwPuWVEhno8CAwEAAQ==
-----END PUBLIC KEY-----`,
		MaxExpiration: 4294967295, // 136 years from now
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	req.Header["Authorization"] = []string{"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjMwMDAwMDAwMDB9.Pgd733jK29m7TlLg9HBVQA2luqQV672Dlf7goVd34EdINZCPdPFNkNqCsg34lZ9A4N_fByJKuT7i-UIPycdD-DoFuaaCX2jyKd9s7Tr4eo1X7gvrENPdJs63AJhlj2lFuiC2_01jjCtxl2z7TslOMDIhFOHTGZwO-fYb4opl_SfN7DrPqadb7C9q3nB_RdPLxF74sNgbLYpLPyvBK7tJlDdmMyIq3VYcYLsdm7Ff4QQltjCoNGLc7drU0_a0s_R9I4wEAww4VoyPM9jNN_94eqathJKL9VndKvM4eTxMNRC26GkXDZg29ExbLbZ7o_JRIW8mGeCSpRX-_ghmqlB7QeGiyuFEprOl8Nok4Cxq5DFePdZWsfHO4mbcuMabXcG45hQ5jX2Nt8hI2E3GmfAXauMqeNfpmkOWtSEF-6ZleTXCKd7PghKCoOfbSSs1Ubq_ktQys3xwcnaNU8F9WthRcsPJSV1ZPpvAOkl87PXhdM-gqoCz8z3uuWk1k5Uynz994r9S30VYrLjLEUrithkE88j1tWBBm0SzdTbpnbkVe4eHY27Q6_UOvcp7s9XN4ShR4grJcQ1Gl8b-0QZ1QX4r8vUd0XBG8TrLxFcBRhMy465i0oj-LYlVGfqKZNAGQIGjzKe6BO7OnOkjtWfuCK-dGLZTTnAZSajLHproFEuQjmo"}

	ctx := pkg.BackgroundContext()
	assert.True(t, jwt.CheckAuthentication(ctx, req))
}

func TestGoodJwtClaimsES256(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm: "ES256",
		Key: `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEMAa5en4smiUzzuOfKKjDrzGW+Hx2
rqGjrzwgkmGypGsfnplZv4okkdfUrPb0VX1PICa0vTotAH97umIvEDBB3Q==
-----END PUBLIC KEY-----`,
		MaxExpiration: 4294967295, // 136 years from now
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	req.Header["Authorization"] = []string{"eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwiaWF0IjoxNTE2MjM5MDEsImV4cCI6MzAwMDAwMDAwMH0.pYto38TFVq6OdyZZdyrNDQObfp1e5_D0VoOQcllZIJHvlzriw_u-peggrzUTXbshTERV03nc-o-jeQsXjpgVOQ"}

	ctx := pkg.BackgroundContext()
	assert.True(t, jwt.CheckAuthentication(ctx, req))
}

func TestGoodJwtClaimsPS256(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm: "PS256",
		Key: `-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAzhZBQzvlt1KBBCJd6dWs
bIS1xNMw4h8ji4+QVVEept2A0RIL5PCTKWPAImtNRDQISzsSvyVA6Pk3/cOvCMLE
tLPQZaibrju8DhQtdK8+7lQk/PteGvwitElWvkIwpJ9hVxUA2CRAu3l6msK+S9V0
eS88UYkihL4XpzzJ1doJQLfB+I8DnHerHuI+qf32XlimLCDzMYj8hMl1RtLJP6YT
EROt3/1zYxCIfT46EcTXJgO7G5ilVWQdDL+ui0GIOVFmA5EqzdVaLhp5xhyffXHb
Y2M9nEYGngcPa8uZnbNsMipWsolP/vUF951GSHruY+oBQZ5RsGov9mpw8MZl+LNv
g2G+aONpHJEZ6iTsMYofj6VJu3O+arLtBu+VTayhjUxcD2h2cnEurV5W5tyCcgFm
BZUXaiwMlBDaFQJBEsb3v0XrJ7H4/CRIipgRf6o/+SIv1FlPkgtyXmLMuWpM7iYA
7OpTLXR4yinBxvWhsot9yhm9ruqs0ZCkOQ0rTn8C5JZwi+tWtQRvp1k8Jv7SS/Dc
0dnAAsRNy4f/DUFaZESdUOvnDTSXt/2VHejkrhc7DeXynTySKMU3t8UqXNHmHZuq
hBcM4yPXyRd2KGlrbwJ/m1VSNjCI6wRkyHvM8ZgYezOHBcvlHlPATPsPK9r+bN7k
vxNWUY5rv006ZwPuWVEhno8CAwEAAQ==
-----END PUBLIC KEY-----`,
		MaxExpiration: 4294967295, // 136 years from now
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	req.Header["Authorization"] = []string{"eyJhbGciOiJQUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjMwMDAwMDAwMDB9.ewoOv1cUimRK0oQAnnPMFtLucgEq5AN0SV4ryD20VgXhXPbtDeQa4e9sPnaWPn3Xi8GMjNXDqCAiNL6U3UKTYZeu7wG5nNX7B-nET1Quelb_sUCBeyoC2a3RHD7D9vFsjN4PpzUY4e-AbL0CmxcorNDcRuJsJ16fjfel_OHmHjfIq1uIHS8f7GQRMhUUFKxA-PzVUVZYGZmYP_4d3TXo7-0mSHGs1Nxsbgq4K8aetUacXl38t0tL5-5z8Lkv1yuVFw4afh0I2eAEpib-_NXpvPCp0grhqQyIEskoEWZrLxdFh4qzprJ9PhCHnqoIz9zCQgL5eNENV3SUJI6OM_RAo9w-YEm6xNQxcLq32R9rM7YTL0Mh11XNHBREEH_GZ0_B-PUSS2zsQpvmdAltgFBTP1bKeEpSCA2YgHhoqAec2-4XqcwfA_JnG3bko0XVKnXkkYMDr1yZ0jOdnX6Rqld2rbRMeTM98QUl9Ik9QzxpbjANsRX3_KwztJlvWUVPur1rpV8sfaVl4FYIYZbcHvAfFe5GJ2PmTcTSdShdRlAMnDNTmH_yo2feMfR0gD2tnE9DxnVrJJTUCP2IXwAF-PtPLqq451jVeC8gJAHy1CJLCmjWKZkQS-vn3k6tQSFOJL_VFPzD75tQmqNvcDl8DpSDbJvaoz4MjkMHTgbGC8JahTg"}

	ctx := pkg.BackgroundContext()
	assert.True(t, jwt.CheckAuthentication(ctx, req))
}

func TestGoodJwtClaimsWithGeohash(t *testing.T) {
	jwtConfig := JWTConfig{
		Algorithm: "RS256",
		Key: `-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAzhZBQzvlt1KBBCJd6dWs
bIS1xNMw4h8ji4+QVVEept2A0RIL5PCTKWPAImtNRDQISzsSvyVA6Pk3/cOvCMLE
tLPQZaibrju8DhQtdK8+7lQk/PteGvwitElWvkIwpJ9hVxUA2CRAu3l6msK+S9V0
eS88UYkihL4XpzzJ1doJQLfB+I8DnHerHuI+qf32XlimLCDzMYj8hMl1RtLJP6YT
EROt3/1zYxCIfT46EcTXJgO7G5ilVWQdDL+ui0GIOVFmA5EqzdVaLhp5xhyffXHb
Y2M9nEYGngcPa8uZnbNsMipWsolP/vUF951GSHruY+oBQZ5RsGov9mpw8MZl+LNv
g2G+aONpHJEZ6iTsMYofj6VJu3O+arLtBu+VTayhjUxcD2h2cnEurV5W5tyCcgFm
BZUXaiwMlBDaFQJBEsb3v0XrJ7H4/CRIipgRf6o/+SIv1FlPkgtyXmLMuWpM7iYA
7OpTLXR4yinBxvWhsot9yhm9ruqs0ZCkOQ0rTn8C5JZwi+tWtQRvp1k8Jv7SS/Dc
0dnAAsRNy4f/DUFaZESdUOvnDTSXt/2VHejkrhc7DeXynTySKMU3t8UqXNHmHZuq
hBcM4yPXyRd2KGlrbwJ/m1VSNjCI6wRkyHvM8ZgYezOHBcvlHlPATPsPK9r+bN7k
vxNWUY5rv006ZwPuWVEhno8CAwEAAQ==
-----END PUBLIC KEY-----`,
		MaxExpiration: 4294967295, // 136 years from now
	}
	jwt, err := JWTRegistration{}.Initialize(jwtConfig, config.ErrorMessages{})

	require.NoError(t, err)
	require.NotNil(t, jwt)

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	require.NoError(t, err)
	require.NotNil(t, req)

	req.Header["Authorization"] = []string{"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjMwMDAwMDAwMDAsImdlb2hhc2giOiJnYnN1djd6In0.x7LjdWIGyxAkH_au1zjHt1hW04yMDCBw-LJqpoHhcsddlIbnXaB3YdOrsWIJ21B9v8HkYYI8xMNgHG91Qp7lJlWprkzqI5vZTmi2GirwB9ImKfjyG9VfJahHEOkFzgXyCw-0p5u0wXiKob5etn3BBQW0_aP56RfKMASCkdeD8nI_udJ1KKEB33i3L4zlnKyuMYXL2z690t0p_qQzm3kUzmqbU5LF8ZHhJGd1F2sziT3rPimEt54M4ArucfYhq2rF-vuOx7NTtSDZnRYlMFvOv7FF0nUe7C-tco1zcp43Z1c9ikWr_ihkq8AzjDayxyHfk7dTI8sfUGsgPX1WzurKQEIvQTRRhGT3ysOpyEx_2aZlNFUyMfjQR2bWFcSntv1Af_qTtwKrCl13PJJq4kxA3lh2hSlL0839JPOUOlSv1NcygkOpKzflOavS0Y04woMLRB1Zq7e2Vt3G_vgopqJJPrPzPZSDO4i5nhFhoWRlwFfz380jatpiE2bUmLGm8lQaugJ_w8MhyPowmAFBzLuygmQo1m27hEhYuTaE4VcMJtPXbOIbYNT3bbHHZBdlbkuZ2PnNkt7o70V4DTIZohc6EscmwG0wBqnfbpAt0b_j0Mm6NROTk1UIAp5JRjz2OPe9O76B21CEO4Q8tIx3VhltfcVowZ_P6ToQ3lg0aLBO5Ig"}

	ctx := pkg.BackgroundContext()
	assert.True(t, jwt.CheckAuthentication(ctx, req))
	ctxAllowedArea, _ := pkg.AllowedAreaFromContext(ctx)
	assert.False(t, ctxAllowedArea.IsNullIsland())
}
