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
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/server"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func init() {
	server.InterruptFlags = append(server.InterruptFlags, syscall.SIGUSR1)

	// This is a hack to help with vscode test execution. Put a .env in repo root w/ anything you need for test containers
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

func coreServeTest(t *testing.T, cfg string, port int, url string) (*http.Response, func(), error) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	rootCmd.SetArgs([]string{"serve", "--raw-config", cfg})

	// This isn't proper goroutine practice but done this way since we only care about errors that happen at startup of the server
	var bindErr error
	exited := false

	go func() {
		bindErr = rootCmd.Execute()
		exited = true
	}()

	if bindErr != nil {
		return nil, nil, bindErr
	}

	time.Sleep(time.Second)

	ok := false
	for i := 1; i < 10; i++ {
		if bindErr != nil {
			return nil, nil, bindErr
		}
		if exited {
			return nil, nil, errors.New("unexpected server exit")
		}

		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 1*time.Second)
		if conn != nil {
			conn.Close()
		}
		if err == nil {
			ok = true
			break
		}

		fmt.Printf("Didn't connect to tcp: %v\n", err)
		time.Sleep(time.Duration(i*i*100) * time.Millisecond)
	}

	if !ok {
		return nil, nil, errors.New("unable to connect to server")
	}

	var err error
	var resp *http.Response

	if url != "" {
		var req *http.Request
		req, err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, nil, err
		}

		resp, err = http.DefaultClient.Do(req)
	}

	return resp, func() {
		err = syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
		require.NoError(t, err)

		if resp != nil {
			resp.Body.Close()
		}
	}, err
}

func Test_ServeCommand_ExecuteInvalidPort(t *testing.T) {

	cfg := `server:
  port: 1
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	_, f, err := coreServeTest(t, cfg, 12340, "http://localhost:12340/") //nolint:bodyclose // Linter doesn't detect this right
	if f != nil {
		defer f()
	}

	require.Error(t, err)
	assert.Equal(t, 1, exitStatus)
}

func Test_ServeCommand_Execute(t *testing.T) {
	cfg := `server:
  port: 12342
  Headers:
    X-Test: result
  RootPath: "/root"
  TilePath: "/tiles"
  Production: false
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
  - id: meta
    provider:
      name: proxy
      url: http://localhost:12342/root/tiles/color/{z}/{x}/{y}?agent={ctx.User-Agent}&key={env.KEY}
`
	t.Setenv("KEY", "hunter2")

	resp, postFunc, err := coreServeTest(t, cfg, 12342, "http://localhost:12342/root/tiles/color/8/12/32") //nolint:bodyclose // Linter doesn't detect this right
	defer postFunc()

	require.NoError(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	assert.Equal(t, "result", resp.Header["X-Test"][0])
	assert.Equal(t, "tilegroxy v0.X.Y", resp.Header["X-Powered-By"][0])

	req, err := http.NewRequest(http.MethodGet, "http://localhost:12342/root/tiles/color/hgkgh/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, "http://localhost:12342/root/tiles/color/8/ghj/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, "http://localhost:12342/root/tiles/color/8/12/dfg", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, "http://localhost:12342/root/tiles/asfas/8/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, "http://localhost:12342/root/tiles/color/800/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, "http://localhost:12342/root/tiles/color/8/1234567/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, "http://localhost:12342/root/tiles/meta/8/1/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, "http://localhost:12342/root", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

func Test_ServeCommand_ExecuteNoContentRoute(t *testing.T) {
	tmpLog, err := os.CreateTemp("", "tilegroxy-test-serve-nocontent-*.log")
	require.NoError(t, err)
	defer os.Remove(tmpLog.Name())

	cfg := `server:
  port: 12341
  Production: true
  timeout: 1
Logging:
  main:
    path: %v
    level: debug
    format: json
    Headers:
      - User-Agent
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
  - id: l
    provider:
      name: custom
      script: |
        package custom

        import (
            "math/rand"
            "strconv"
            "strings"
            "time"

            "tilegroxy/tilegroxy"
        )
        func preAuth(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, params map[string]interface{}, cientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages,
        )  (tilegroxy.ProviderContext, error) {
            return tilegroxy.ProviderContext{AuthBypass: true}, nil
        }

        func generateTile(ctx tilegroxy.Context, providerContext tilegroxy.ProviderContext, tileRequest tilegroxy.TileRequest, params map[string]interface{}, clientConfig tilegroxy.ClientConfig, errorMessages tilegroxy.ErrorMessages ) (*tilegroxy.Image, error ) {
            time.Sleep(10 * time.Second)
            return &[]byte{0x01,0x02}, nil
        }
`
	cfg = fmt.Sprintf(cfg, tmpLog.Name())

	resp, postFunc, err := coreServeTest(t, cfg, 12341, "http://localhost:12341/") //nolint:bodyclose // Linter doesn't detect this right
	defer postFunc()

	require.NoError(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 204, resp.StatusCode)

	req, err := http.NewRequest(http.MethodGet, "http://localhost:12341/tiles/color/8/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Nil(t, resp.Header["X-Powered-By"])
	resp.Body.Close()

	fileInfo, err := os.Stat(tmpLog.Name())
	require.NoError(t, err)
	assert.NotZero(t, fileInfo.Size())

	req, err = http.NewRequest(http.MethodGet, "http://localhost:12341/tiles/l/8/12/32", nil)
	require.NoError(t, err)

	start := time.Now()
	resp2, _ := http.DefaultClient.Do(req)
	end := time.Now()
	assert.Greater(t, 2.0, end.Sub(start).Seconds())
	if resp2 != nil {
		assert.Equal(t, 503, resp2.StatusCode)
		if resp2.Body != nil {
			assert.NoError(t, resp2.Body.Close())
		}
	}
}

func setupEtcd(ctx context.Context) (testcontainers.Container, error) {

	p, _ := nat.NewPort("tcp", "2379")
	etcdReq := testcontainers.ContainerRequest{
		Image: "bitnami/etcd:latest",
		WaitingFor: wait.ForAll(
			wait.ForLog("ready to serve client requests"),
			wait.ForListeningPort(p),
		),
		ExposedPorts: []string{"2379"},
		Env: map[string]string{
			"ALLOW_NONE_AUTHENTICATION": "yes",
		},
	}

	return testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: etcdReq,
		Started:          true,
	})
}

func Test_ServeCommand_RemoteProvider(t *testing.T) {
	exitStatus = -1
	rootCmd.ResetFlags()
	serveCmd.ResetFlags()
	initRoot()
	initServe()

	ctx := context.Background()

	var endpoint string
	var etcdC testcontainers.Container
	var err error

	for range 3 {
		etcdC, err = setupEtcd(ctx)
		time.Sleep(time.Second)
		if err == nil {
			endpoint, err = etcdC.Endpoint(ctx, "")
		}
		if err == nil && etcdC != nil {
			break
		}
		if etcdC != nil {
			err = etcdC.Terminate(ctx)
		}
	}

	require.NoError(t, err)

	defer func() {
		require.NoError(t, etcdC.Terminate(ctx))
	}()

	cfg := `server:
  port: 12342
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	fmt.Println("Running on " + endpoint)

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer cli.Close()
	ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err = cli.Put(ctx2, "sample_key", cfg)
	cancel()
	require.NoError(t, err)

	rootCmd.SetArgs([]string{"serve", "--remote-provider", "etcd3", "--remote-path", "sample_key", "--remote-endpoint", "http://" + endpoint})

	go func() { assert.NoError(t, rootCmd.Execute()) }()

	time.Sleep(1 * time.Second)

	req, err := http.NewRequest(http.MethodGet, "http://localhost:12342/tiles/color/8/12/32", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)

	defer func() {
		require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR1))
		if resp != nil {
			resp.Body.Close()
		}
	}()

	require.NoError(t, err)
	if assert.NotNil(t, resp) {
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	}
}
