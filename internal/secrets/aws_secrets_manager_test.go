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
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	//This is a hack to help with vscode test execution. Put a .env in repo root w/ anything you need for test containers
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

func Test_SecretManager_Validate(t *testing.T) {
	s, err := AWSSecretsManagerSecreter{}.Initialize(AWSSecretsManagerConfig{
		Access:  "asffasfa",
		Secret:  "asfasfas",
		Region:  "safasfasfasf",
		Profile: "sfjasklfjaslkfjla",
	}, config.ErrorMessages{})

	assert.Error(t, err)
	assert.Nil(t, s)
}

func Test_SecretManager_Execute(t *testing.T) {
	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "localstack/localstack",
		ExposedPorts: []string{"4566/tcp"},
		Privileged:   true,
		WaitingFor:   wait.ForAll(wait.ForLog("Ready"), wait.ForListeningPort(nat.Port("4566/tcp"))),
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		assert.NoError(t, err)
	}
	defer func(c testcontainers.Container, ctx context.Context) {
		if err := c.Terminate(ctx); err != nil {
			fmt.Println(err)
		}
	}(c, ctx)

	endpoint, err := c.PortEndpoint(ctx, nat.Port("4566/tcp"), "http")
	assert.NoError(t, err)

	so, err := AWSSecretsManagerSecreter{}.Initialize(AWSSecretsManagerConfig{
		Access:   "",
		Secret:   "",
		Region:   "us-east-1",
		Endpoint: endpoint,
	}, config.ErrorMessages{})
	s := so.(*AWSSecretsManager)

	assert.NoError(t, err)

	err = s.makeSecret("test", "test")
	assert.NoError(t, err)
	v, err := s.Lookup("test")
	assert.NoError(t, err)
	assert.Equal(t, "test", v)
	v2, err := s.Lookup("test")
	assert.NoError(t, err)
	assert.Equal(t, v, v2)

	err = s.makeSecret("test2", `{"key":"val"}`)
	assert.NoError(t, err)
	v3, err := s.Lookup("test2:key")
	assert.NoError(t, err)
	assert.Equal(t, "val", v3)

	so, err = AWSSecretsManagerSecreter{}.Initialize(AWSSecretsManagerConfig{
		Access:   "",
		Secret:   "",
		Region:   "us-east-1",
		Endpoint: endpoint,
		TTL:      -1,
	}, config.ErrorMessages{})
	assert.NoError(t, err)
	s = so.(*AWSSecretsManager)

	v4, err := s.Lookup("test2:key")
	assert.NoError(t, err)
	assert.Equal(t, "val", v4)
}
