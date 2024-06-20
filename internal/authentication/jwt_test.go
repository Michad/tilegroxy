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

package authentication

import (
	"net/http"
	"testing"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/stretchr/testify/assert"
)

//note: JWTs here will expire in the year 2065. They will need to be updated on the off-chance this is still used 40 years from now

func TestFailMissingArgs(t *testing.T) {
	jwtConfig := JwtConfig{}
	jwt, err := ConstructJwt(&jwtConfig, &config.ErrorMessages{})

	assert.NotNil(t, err)
	assert.Nil(t, jwt)
}
func TestFailMissingKey(t *testing.T) {
	jwtConfig := JwtConfig{
		Algorithm: "HS256",
	}
	jwt, err := ConstructJwt(&jwtConfig, &config.ErrorMessages{})

	assert.NotNil(t, err)
	assert.Nil(t, jwt)
}
func TestFailMissingAlg(t *testing.T) {
	jwtConfig := JwtConfig{
		VerificationKey: "hunter2",
	}
	jwt, err := ConstructJwt(&jwtConfig, &config.ErrorMessages{})

	assert.NotNil(t, err)
	assert.Nil(t, jwt)
}

func TestGoodJwts(t *testing.T) {
	jwtConfig := JwtConfig{
		Algorithm:             "HS256",
		VerificationKey:       "hunter2",
		MaxExpirationDuration: 4294967295, //136 years from now
	}
	jwt, err := ConstructJwt(&jwtConfig, &config.ErrorMessages{})

	if !assert.Nil(t, err) || !assert.NotNil(t, jwt) {
		return
	}

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	if !assert.Nil(t, err) || !assert.NotNil(t, req) {
		return
	}

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjMwMDAwMDAwMDB9.npKpCaeyhdn-CsbEc_AuPz3Nkmpeh6K73SYCaBMqWoE"} //Valid JWT with same key with expiration in the distant future
	assert.True(t, jwt.CheckAuthentication(req))
}

func TestBadJwts(t *testing.T) {
	jwtConfig := JwtConfig{
		Algorithm:       "HS256",
		VerificationKey: "hunter2",
	}
	jwt, err := ConstructJwt(&jwtConfig, &config.ErrorMessages{})

	if !assert.Nil(t, err) || !assert.NotNil(t, jwt) {
		return
	}

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	if !assert.Nil(t, err) || !assert.NotNil(t, req) {
		return
	}

	req.Header["Authorization"] = []string{"unparseable"}
	assert.False(t, jwt.CheckAuthentication(req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.TsbkW6Baw6npF0SUva-SdB9gZ9MLtLFUMu3BtUnspzk"} //Valid JWT but with a different key
	assert.False(t, jwt.CheckAuthentication(req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.sCuKLbsVsWuzV45ZtOEslD0WHPyPYa4gkEBZNP084os"} //Valid JWT with same key but no expiration
	assert.False(t, jwt.CheckAuthentication(req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjMwMDAwMDAwMDB9.npKpCaeyhdn-CsbEc_AuPz3Nkmpeh6K73SYCaBMqWoE"} //Valid JWT with same key with expiration in the distant future
	assert.False(t, jwt.CheckAuthentication(req))
}

func TestGoodJwtClaims(t *testing.T) {
	jwtConfig := JwtConfig{
		Algorithm:             "HS256",
		VerificationKey:       "hunter2",
		MaxExpirationDuration: 4294967295, //136 years from now
		ExpectedAudience:      "audience",
		ExpectedSubject:       "subject",
		ExpectedIssuer:        "issuer",
		ExpectedScope:         "tile",
	}
	jwt, err := ConstructJwt(&jwtConfig, &config.ErrorMessages{})

	if !assert.Nil(t, err) || !assert.NotNil(t, jwt) {
		return
	}

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	if !assert.Nil(t, err) || !assert.NotNil(t, req) {
		return
	}

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJpc3N1ZXIiLCJzY29wZSI6InNvbWV0aGluZyB0aWxlIG90aGVyIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjQyOTQ5NjcyOTV9.6jOBwjsvFcJXGkaleXB-75F6J3CjaQYuRELJPfvOfQE"} //Valid JWT with all claims
	assert.True(t, jwt.CheckAuthentication(req))
}

func TestBadJwtClaims(t *testing.T) {
	jwtConfig := JwtConfig{
		Algorithm:             "HS256",
		VerificationKey:       "hunter2",
		MaxExpirationDuration: 4294967295, //136 years from now
		ExpectedAudience:      "audience",
		ExpectedSubject:       "subject",
		ExpectedIssuer:        "issuer",
		ExpectedScope:         "tile",
	}
	jwt, err := ConstructJwt(&jwtConfig, &config.ErrorMessages{})

	if !assert.Nil(t, err) || !assert.NotNil(t, jwt) {
		return
	}

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/tiles/layer/0/0/0", nil)

	if !assert.Nil(t, err) || !assert.NotNil(t, req) {
		return
	}

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYmFkIiwiaXNzIjoiaXNzdWVyIiwic2NvcGUiOiJzb21ldGhpbmcgdGlsZSBvdGhlciIsIm5hbWUiOiJKb2huIERvZSIsImlhdCI6MTUxNjIzOTAyMiwiZXhwIjo0Mjk0OTY3Mjk1fQ.1_i6c0LLPoQWrEB-Y1wJiEiKoCAwGRc3wE0FoFelcKQ"} // Invalid aud
	assert.False(t, jwt.CheckAuthentication(req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJiYWQiLCJhdWQiOiJhdWRpZW5jZSIsImlzcyI6Imlzc3VlciIsInNjb3BlIjoic29tZXRoaW5nIHRpbGUgb3RoZXIiLCJuYW1lIjoiSm9obiBEb2UiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6NDI5NDk2NzI5NX0.TtVgpJfEVEjTe6Z8FIHCiSqVsKD00MHL7OBDuLh78hw"} // Invalid sub
	assert.False(t, jwt.CheckAuthentication(req))

	req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJub2JvZHkgd2lsbCBldmVyIHJlYWQgdGhpcyIsInNjb3BlIjoic29tZXRoaW5nIHRpbGUgb3RoZXIiLCJuYW1lIjoiSm9obiBEb2UiLCJpYXQiOjE1MTYyMzkwMjIsImV4cCI6NDI5NDk2NzI5NX0.IeaRecjpT4pQm6AUpJUoCUQskyGkcGXXab-Bccc2q3I"} // Invalid iss
	assert.False(t, jwt.CheckAuthentication(req))

	//TODO: when implemented
	// req.Header["Authorization"] = []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJpc3N1ZXIiLCJzY29wZSI6InNvbWV0aGluZyBvdGhlciIsIm5hbWUiOiJKb2huIERvZSIsImlhdCI6MTUxNjIzOTAyMiwiZXhwIjo0Mjk0OTY3Mjk1fQ.yt4Ga01Mn5wIUglH67gPr4NEt4g9AlwEFiTy8YNN-8g"} // Invalid scope
	// assert.False(t, jwt.Preauth(req))
}
