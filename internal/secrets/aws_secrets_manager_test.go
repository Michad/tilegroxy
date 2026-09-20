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

//go:build !unit

package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/secret"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	// This is a hack to help with vscode test execution. Put a .env in repo root w/ anything you need for test containers
	if env, err := os.ReadFile("../../.env"); err == nil {
		envs := strings.Split(string(env), "\n")
		for _, e := range envs {
			if es := strings.Split(e, "="); len(es) == 2 {
				fmt.Printf("Loading env...")
				os.Setenv(es[0], es[1])
			}
		}
	}
}

func newLocalstackSecretManager(ctx context.Context, t *testing.T) *AWSSecretsManager {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "localstack/localstack:3.8.1",
		ExposedPorts: []string{"4566/tcp"},
		Privileged:   true,
		WaitingFor:   wait.ForAll(wait.ForLog("Ready"), wait.ForListeningPort("4566/tcp")),
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := c.Terminate(ctx); err != nil {
			fmt.Println(err)
		}
	})

	endpoint, err := c.PortEndpoint(ctx, "4566/tcp", "http")
	require.NoError(t, err)

	so, err := AWSSecretsManagerSecreter{}.Initialize(AWSSecretsManagerConfig{
		Access:   "a",
		Secret:   "a",
		Region:   "us-east-1",
		Endpoint: endpoint,
	}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{}})
	require.NoError(t, err)

	return so.(*AWSSecretsManager)
}

func Test_SecretManager_Validate(t *testing.T) {
	s, err := AWSSecretsManagerSecreter{}.Initialize(AWSSecretsManagerConfig{
		Access:  "asffasfa",
		Secret:  "asfasfas",
		Region:  "safasfasfasf",
		Profile: "sfjasklfjaslkfjla",
	}, secret.SecreterDeps{ErrorMessages: config.ErrorMessages{}})

	require.Error(t, err)
	assert.Nil(t, s)
}

func Test_SecretManager_Execute(t *testing.T) {
	ctx := context.Background()
	s := newLocalstackSecretManager(ctx, t)

	require.NoError(t, s.makeSecret("test", "test"))

	v, _, err := s.Lookup(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, "test", v)

	v2, _, err := s.Lookup(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, v, v2)

	require.NoError(t, s.makeSecret("test2", `{"key":"val"}`))

	v3, _, err := s.Lookup(ctx, "test2:key")
	require.NoError(t, err)
	assert.Equal(t, "val", v3)
}

// Two keys inside one JSON secret must cost a single GetSecretValue, which the name-level map is
// the only thing providing now that the operator-facing cache lives in the wrapper
func Test_SecretManager_JSONKeysShareOneFetch(t *testing.T) {
	ctx := context.Background()
	s := newLocalstackSecretManager(ctx, t)

	require.NoError(t, s.makeSecret("shared", `{"user":"u","password":"p"}`))

	user, v1, err := s.Lookup(ctx, "shared:user")
	require.NoError(t, err)
	assert.Equal(t, "u", user)

	password, v2, err := s.Lookup(ctx, "shared:password")
	require.NoError(t, err)
	assert.Equal(t, "p", password)

	assert.Equal(t, v1, v2, "both keys resolve to the same secret version")
}

func Test_SecretManager_CheckReportsCurrentVersion(t *testing.T) {
	ctx := context.Background()
	s := newLocalstackSecretManager(ctx, t)

	require.NoError(t, s.makeSecret("rotating", "before"))

	value, version, err := s.Lookup(ctx, "rotating")
	require.NoError(t, err)
	assert.Equal(t, "before", value)
	require.NotEmpty(t, version)

	// Check on an unchanged secret must agree with what Lookup recorded, which is what proves the
	// AWSCURRENT selection matches GetSecretValue's default
	versions, err := s.Check(ctx, []string{"rotating"})
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, version, versions[0])
}

func Test_SecretManager_CheckSeesRotation(t *testing.T) {
	ctx := context.Background()
	s := newLocalstackSecretManager(ctx, t)

	require.NoError(t, s.makeSecret("rotating2", "before"))

	_, version, err := s.Lookup(ctx, "rotating2")
	require.NoError(t, err)

	_, err = s.client.UpdateSecret(ctx, &secretsmanager.UpdateSecretInput{
		SecretId:     aws.String("rotating2"),
		SecretString: aws.String("after"),
	})
	require.NoError(t, err)

	versions, err := s.Check(ctx, []string{"rotating2"})
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.NotEqual(t, version, versions[0], "a rotated secret reports a new version")
}

// Sibling JSON keys collapse to one DescribeSecret and report the same version
func Test_SecretManager_CheckDeduplicatesSecretNames(t *testing.T) {
	ctx := context.Background()
	s := newLocalstackSecretManager(ctx, t)

	require.NoError(t, s.makeSecret("shared2", `{"user":"u","password":"p"}`))

	versions, err := s.Check(ctx, []string{"shared2:user", "shared2:password"})
	require.NoError(t, err)
	require.Len(t, versions, 2)
	assert.Equal(t, versions[0], versions[1])
	assert.NotEmpty(t, versions[0])
}

func Test_SecretManager_CheckOnMissingSecretErrors(t *testing.T) {
	ctx := context.Background()
	s := newLocalstackSecretManager(ctx, t)

	_, err := s.Check(ctx, []string{"does-not-exist"})
	require.Error(t, err)
}
