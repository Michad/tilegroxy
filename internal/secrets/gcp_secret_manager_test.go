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

package secrets

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_GCPSecretManager_Validate(t *testing.T) {
	s, err := GCPSecretManagerSecreter{}.Initialize(GCPSecretManagerConfig{}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{ParamRequired: "%v is required"}})

	require.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "secret.gcpsecretmanager.project")
}

// fakeSecretManagerServer mimics just enough of the Secret Manager REST API for AccessSecretVersion
func fakeSecretManagerServer(t *testing.T, secrets map[string]string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path is /v1/projects/{project}/secrets/{id}/versions/{version}:access
		path := strings.TrimSuffix(r.URL.Path, ":access")
		parts := strings.Split(strings.TrimPrefix(path, "/v1/"), "/")
		if len(parts) != 6 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		secretID := parts[3]

		val, ok := secrets[secretID]
		if !ok {
			http.Error(w, `{"error":{"code":404,"message":"not found"}}`, http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":%q,"payload":{"data":%q}}`, path, base64.StdEncoding.EncodeToString([]byte(val)))
	}))
}

func Test_GCPSecretManager_Execute(t *testing.T) {
	srv := fakeSecretManagerServer(t, map[string]string{
		"test":  "test",
		"test2": `{"key":"val"}`,
	})
	defer srv.Close()

	so, err := GCPSecretManagerSecreter{}.Initialize(GCPSecretManagerConfig{
		Project:  "my-project",
		Endpoint: srv.URL,
	}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	s := so.(*GCPSecretManager)

	v, err := s.Lookup("test")
	require.NoError(t, err)
	assert.Equal(t, "test", v)

	// Second call should be served from cache; same result
	v2, err := s.Lookup("test")
	require.NoError(t, err)
	assert.Equal(t, v, v2)

	v3, err := s.Lookup("test2:key")
	require.NoError(t, err)
	assert.Equal(t, "val", v3)

	so, err = GCPSecretManagerSecreter{}.Initialize(GCPSecretManagerConfig{
		Project:  "my-project",
		Endpoint: srv.URL,
		TTL:      -1,
	}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	s = so.(*GCPSecretManager)

	v4, err := s.Lookup("test2:key")
	require.NoError(t, err)
	assert.Equal(t, "val", v4)
}

func Test_GCPSecretManager_Execute_NotFound(t *testing.T) {
	srv := fakeSecretManagerServer(t, map[string]string{})
	defer srv.Close()

	so, err := GCPSecretManagerSecreter{}.Initialize(GCPSecretManagerConfig{
		Project:  "my-project",
		Endpoint: srv.URL,
	}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	s := so.(*GCPSecretManager)

	_, err = s.Lookup("missing")
	require.Error(t, err)
}

func Test_GCPSecretManager_Execute_CustomVersionAndSeparator(t *testing.T) {
	srv := fakeSecretManagerServer(t, map[string]string{
		"test": "pinned",
	})
	defer srv.Close()

	so, err := GCPSecretManagerSecreter{}.Initialize(GCPSecretManagerConfig{
		Project:  "my-project",
		Endpoint: srv.URL,
		Version:  "3",
		TTL:      -1,
	}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	s := so.(*GCPSecretManager)

	v, err := s.Lookup("test")
	require.NoError(t, err)
	assert.Equal(t, "pinned", v)
}

// Test_GCPSecretManager_Execute_KeyWithVersion covers the id:key:version form, where the key itself
// pins a version rather than relying on the config default
func Test_GCPSecretManager_Execute_KeyWithVersion(t *testing.T) {
	srv := fakeSecretManagerServer(t, map[string]string{
		"test2": `{"key":"val"}`,
	})
	defer srv.Close()

	so, err := GCPSecretManagerSecreter{}.Initialize(GCPSecretManagerConfig{
		Project:  "my-project",
		Endpoint: srv.URL,
		TTL:      -1,
	}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	s := so.(*GCPSecretManager)

	v, err := s.Lookup("test2:key:5")
	require.NoError(t, err)
	assert.Equal(t, "val", v)
}

// writeFakeServiceAccountFile writes a syntactically valid (but unusable) service account key file so
// Initialize's CredentialsFile branch can be exercised without a real GCP credential
func writeFakeServiceAccountFile(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	sa := map[string]string{
		"type":         "service_account",
		"project_id":   "my-project",
		"private_key":  string(keyPEM),
		"client_email": "fake@my-project.iam.gserviceaccount.com",
		"token_uri":    "https://oauth2.googleapis.com/token",
	}

	b, err := json.Marshal(sa)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "fake-sa.json")
	require.NoError(t, os.WriteFile(path, b, 0o600))

	return path
}

func Test_GCPSecretManager_Execute_CredentialsFile(t *testing.T) {
	srv := fakeSecretManagerServer(t, map[string]string{
		"test": "test",
	})
	defer srv.Close()

	path := writeFakeServiceAccountFile(t)

	so, err := GCPSecretManagerSecreter{}.Initialize(GCPSecretManagerConfig{
		Project:         "my-project",
		Endpoint:        srv.URL,
		CredentialsFile: path,
		TTL:             -1,
	}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)
	s := so.(*GCPSecretManager)
	assert.Equal(t, path, s.CredentialsFile)
}
