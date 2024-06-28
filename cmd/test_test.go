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

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	//This is a hack to help with vscode test execution. Put a .env in repo root w/ anything you need for test containers
	if env, err := os.ReadFile("../.env"); err == nil {
		envs := strings.Split(string(env), "\n")
		for _, e := range envs {
			if es := strings.Split(e, "="); len(es) == 2 {
				fmt.Printf("Loading env...")
				os.Setenv(es[0], es[1])
			}
		}
	}
}

func Test_ExecuteTestCommandNoCache(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	testCmd.ResetFlags()
	initRoot()
	initTest()
	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"test", "-c", "../examples/configurations/simple.json", "--no-cache"})
	assert.Nil(t, cmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.Greater(t, len(out), 69)
	assert.Less(t, exitStatus, 1)
}

func Test_ExecuteTestCommand(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	testCmd.ResetFlags()
	initRoot()
	initTest()

	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"test", "-c", "../examples/configurations/simple.json"})
	assert.Nil(t, cmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.Greater(t, len(out), 69)
	assert.Equal(t, exitStatus, 1)
}

func Test_ExecuteTestWithMultiCache(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	testCmd.ResetFlags()
	initRoot()
	initTest()

	dir, err := os.MkdirTemp(os.TempDir(), "tilegroxy-tests")
	defer os.RemoveAll(dir)
	assert.Nil(t, err)

	cfg := fmt.Sprintf(
		`cache:
  name: multi
  tiers:
    - name: memory
      maxsize: 100
      ttl: 1000
    - name: disk
      path: %v
layers:
  - id: osm
    provider:
        name: proxy
        url: https://tile.openstreetmap.org/{z}/{x}/{y}.png
`, dir)

	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"test", "--raw-config", cfg})
	assert.Nil(t, cmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(out)

	fmt.Println(outStr)

	assert.NotContains(t, outStr, "Warning:")

	assert.Greater(t, len(outStr), 69)
	assert.Less(t, exitStatus, 1)
}

func Test_ExecuteTestWithRedisCache(t *testing.T) {

	exitStatus = -1
	rootCmd.ResetFlags()
	testCmd.ResetFlags()
	initRoot()
	initTest()

	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "redis:latest",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}
	redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if !assert.Nil(t, err) {
		return
	}

	defer redisC.Terminate(ctx)

	endpoint, err := redisC.Endpoint(ctx, "")
	if !assert.Nil(t, err) {
		return
	}
	endSplit := strings.Split(endpoint, ":")

	cfg := fmt.Sprintf(
		`cache:
  name: redis
  host: %v
  port: %v
layers:
  - id: osm
    provider:
        name: proxy
        url: https://tile.openstreetmap.org/{z}/{x}/{y}.png
`, endSplit[0], endSplit[1])
	fmt.Println(cfg)

	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"test", "--raw-config", cfg})
	assert.Nil(t, cmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(out)

	fmt.Println(outStr)

	assert.NotContains(t, outStr, "Warning:")

	assert.Greater(t, len(outStr), 69)
	assert.Less(t, exitStatus, 1)
}
