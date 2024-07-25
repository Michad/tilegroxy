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

//go:build !unit && !no_aws

package caches

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/docker/go-connections/nat"
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

func Test_S3Validate(t *testing.T) {
	s3, err := S3Registration{}.Initialize(S3Config{}, config.ErrorMessages{})

	assert.Nil(t, s3)
	require.Error(t, err)

	s3, err = S3Registration{}.Initialize(S3Config{Bucket: "test", Access: "AJIASAFASF"}, config.ErrorMessages{})

	assert.Nil(t, s3)
	require.Error(t, err)

	s3, err = S3Registration{}.Initialize(S3Config{Bucket: "test", Access: "AJIASAFASF", Secret: "hunter2", StorageClass: "fakeyfake"}, config.ErrorMessages{})

	assert.Nil(t, s3)
	require.Error(t, err)
}

func Test_S3ValidateProfile(t *testing.T) {
	// Currently invalid profile fails when using it for the first time vs on construct. Would rather have it fail in constructor but not sure how to best validate that without potentially impacting s3-compatible use cases. For now leaving this test assuming the failure happens in one of two places
	var err2 error
	s3, err1 := S3Registration{}.Initialize(S3Config{Bucket: "test", Profile: "fakeyfake"}, config.ErrorMessages{})
	if s3 != nil {
		_, err2 = s3.Lookup(pkg.TileRequest{})
	}

	require.Error(t, errors.Join(err1, err2))
}

func Test_S3Execute(t *testing.T) {
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
		require.NoError(t, err)
	}
	defer func(c testcontainers.Container, ctx context.Context) {
		if err := c.Terminate(ctx); err != nil {
			fmt.Println(err)
		}
	}(c, ctx)

	endpoint, err := c.PortEndpoint(ctx, nat.Port("4566/tcp"), "http")
	require.NoError(t, err)

	s3, err := S3Registration{}.Initialize(S3Config{
		Access:       "test",
		Secret:       "test",
		Bucket:       "test",
		Endpoint:     endpoint, // "http://localhost:4566",
		Region:       "us-east-1",
		UsePathStyle: true,
	}, config.ErrorMessages{})

	assert.NotNil(t, s3)
	require.NoError(t, err)

	err = s3.(*S3).makeBucket()
	require.NoError(t, err)

	validateSaveAndLookup(t, s3)
	img, err := s3.Lookup(pkg.TileRequest{LayerName: "layer", Z: 93, X: 53, Y: 12345})
	assert.Nil(t, img)
	require.NoError(t, err)
}
