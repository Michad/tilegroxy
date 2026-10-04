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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/server"
	"github.com/Michad/tilegroxy/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func init() {
	server.InterruptFlags = append(server.InterruptFlags, syscall.SIGUSR1)

	// Lets vscode test runs pick up testcontainer settings from a .env in the repo root
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

func coreServeTest(t *testing.T, cfg string, port int, url string, hotReload bool) (*http.Response, func(), error) {
	exitStatus = -1
	rootCmd.ResetFlags()
	serveCmd.ResetFlags()
	initRoot()
	initServe()

	if _, err := os.Stat(cfg); err == nil {
		args := []string{"serve", "-c", cfg}
		if hotReload {
			args = append(args, "--hot-reload")
		}
		rootCmd.SetArgs(args)
	} else {
		rootCmd.SetArgs([]string{"serve", "--raw-config", cfg})
	}

	// Only errors at server startup matter here, hence the shortcut
	var mu sync.Mutex
	var bindErr error
	exited := false
	done := make(chan struct{})

	go func() {
		defer close(done)
		err := rootCmd.Execute()
		mu.Lock()
		bindErr = err
		exited = true
		mu.Unlock()
	}()

	// Keeps shared package state (rootCmd, exitStatus) from racing the next test
	waitForExit := func() {
		syscall.Kill(syscall.Getpid(), syscall.SIGUSR1) //nolint:errcheck
		<-done
	}

	getState := func() (error, bool) {
		mu.Lock()
		defer mu.Unlock()
		return bindErr, exited
	}

	if err, _ := getState(); err != nil {
		<-done
		return nil, nil, err
	}

	time.Sleep(time.Second)

	ok := false
	for i := 1; i < 10; i++ {
		if err, exited := getState(); err != nil {
			<-done
			return nil, nil, err
		} else if exited {
			<-done
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
		waitForExit()
		return nil, nil, errors.New("unable to connect to server")
	}

	var err error
	var resp *http.Response

	if url != "" {
		var req *http.Request
		req, err = http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			waitForExit()
			return nil, nil, err
		}

		resp, err = http.DefaultClient.Do(req)
	}

	return resp, func() {
		killErr := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
		require.NoError(t, killErr)

		if resp != nil {
			resp.Body.Close()
		}

		<-done
	}, err
}

// Returns 0 instead of failing so it can be polled mid-reload
func tileStatus(url string) int {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()

	return resp.StatusCode
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

	port := testutil.FreePort(t)

	_, f, err := coreServeTest(t, cfg, port, fmt.Sprintf("http://localhost:%d/", port), true) //nolint:bodyclose // Linter doesn't detect this right
	if f != nil {
		defer f()
	}

	require.Error(t, err)
	assert.Equal(t, 1, exitStatus)
}

func Test_ServeCommand_Execute(t *testing.T) {
	port := testutil.FreePort(t)
	base := fmt.Sprintf("http://localhost:%d", port)

	cfg := fmt.Sprintf(`server:
  port: %[1]d
  Headers:
    X-Test: result
  RootPath: "/root"
  TilePath: "/tiles"
  Production: false
telemetry:
  enabled: true
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
  - id: meta
    provider:
      name: proxy
      url: http://localhost:%[1]d/root/tiles/color/{z}/{x}/{y}?agent={ctx.User-Agent}&key={env.KEY}
`, port)
	t.Setenv("KEY", "hunter2")

	resp, postFunc, err := coreServeTest(t, cfg, port, base+"/root/tiles/color/8/12/32", true) //nolint:bodyclose // Linter doesn't detect this right
	defer postFunc()

	require.NoError(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	assert.Equal(t, "result", resp.Header["X-Test"][0])
	assert.Equal(t, "tilegroxy v0.X.Y", resp.Header["X-Powered-By"][0])

	req, err := http.NewRequest(http.MethodGet, base+"/root/tiles/color/hgkgh/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, base+"/root/tiles/color/8/ghj/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, base+"/root/tiles/color/8/12/dfg", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, base+"/root/tiles/asfas/8/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, base+"/root/tiles/color/800/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, base+"/root/tiles/color/8/1234567/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, base+"/root/tiles/meta/8/1/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	req, err = http.NewRequest(http.MethodGet, base+"/root", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

func Test_ServeCommand_Reload(t *testing.T) {
	cfgDir := t.TempDir()
	cfgFile := cfgDir + string(os.PathSeparator) + "test_servecommand_reload.yml"

	port := testutil.FreePort(t)
	base := fmt.Sprintf("http://localhost:%d", port)

	cfg1 := fmt.Sprintf(`server:
  port: %d
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`, port)
	cfg2 := strings.Replace(cfg1, "id: color", "id: color2", 1)
	cfgInvalid := `asfasfasfasflkasfjaslfjlasasfjlkafkf`

	err := os.WriteFile(cfgFile, []byte(cfg1), 0600)
	require.NoError(t, err)

	resp, postFunc, err := coreServeTest(t, cfgFile, port, base+"/tiles/color/8/12/32", true) //nolint:bodyclose // Linter doesn't detect this right
	defer postFunc()

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])

	err = os.WriteFile(cfgFile, []byte(cfg2), 0600)
	require.NoError(t, err)

	require.Eventually(t, func() bool { return tileStatus(base+"/tiles/color2/8/12/32") == http.StatusOK }, 15*time.Second, 100*time.Millisecond)
	assert.Equal(t, http.StatusUnauthorized, tileStatus(base+"/tiles/color/8/12/32"))

	req, err := http.NewRequest(http.MethodGet, base+"/tiles/color2/8/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	resp.Body.Close()

	// A broken config on reload should leave the old config running
	err = os.WriteFile(cfgFile, []byte(cfgInvalid), 0600)
	require.NoError(t, err)
	time.Sleep(time.Second * 4) // Nothing signals a rejected reload, so give it time to wrongly take effect

	req, err = http.NewRequest(http.MethodGet, base+"/tiles/color2/8/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	resp.Body.Close()
}

// Without --hot-reload a file change is ignored until SIGHUP asks for it
func Test_ServeCommand_ReloadsOnSighup(t *testing.T) {
	cfgFile := filepath.Join(t.TempDir(), "test_servecommand_reload_on_signal.yml")

	port := testutil.FreePort(t)

	cfg1 := fmt.Sprintf(`server:
  port: %d
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`, port)
	cfg2 := strings.Replace(cfg1, "id: color", "id: color2", 1)

	require.NoError(t, os.WriteFile(cfgFile, []byte(cfg1), 0600))

	resp, postFunc, err := coreServeTest(t, cfgFile, port, fmt.Sprintf("http://localhost:%d/tiles/color/8/12/32", port), false) //nolint:bodyclose // Linter doesn't detect this right
	defer postFunc()

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	status := func(layer string) int {
		return tileStatus(fmt.Sprintf("http://localhost:%d/tiles/%s/8/12/32", port, layer))
	}

	require.NoError(t, os.WriteFile(cfgFile, []byte(cfg2), 0600))
	time.Sleep(3 * time.Second)
	assert.Equal(t, http.StatusOK, status("color"), "a file change alone must not reload")

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))

	assert.Eventually(t, func() bool { return status("color2") == http.StatusOK }, 10*time.Second, 100*time.Millisecond)
	assert.Equal(t, http.StatusUnauthorized, status("color"))
}

func Test_ServeCommand_ExecuteNoContentRoute(t *testing.T) {
	tmpLog, err := os.CreateTemp("", "tilegroxy-test-serve-nocontent-*.log")
	require.NoError(t, err)
	defer os.Remove(tmpLog.Name())

	port := testutil.FreePort(t)
	base := fmt.Sprintf("http://localhost:%d", port)

	cfg := `server:
  port: %d
  Production: true
  timeout: 1
Logging:
  main:
    path: %s
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
            return &tilegroxy.Image{Content:[]byte{0x01,0x02}}, nil
        }
`
	cfg = fmt.Sprintf(cfg, port, tmpLog.Name())

	resp, postFunc, err := coreServeTest(t, cfg, port, base+"/", true) //nolint:bodyclose // Linter doesn't detect this right
	defer postFunc()

	require.NoError(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 204, resp.StatusCode)

	req, err := http.NewRequest(http.MethodGet, base+"/tiles/color/8/12/32", nil)
	require.NoError(t, err)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Nil(t, resp.Header["X-Powered-By"])
	resp.Body.Close()

	fileInfo, err := os.Stat(tmpLog.Name())
	require.NoError(t, err)
	assert.NotZero(t, fileInfo.Size())

	req, err = http.NewRequest(http.MethodGet, base+"/tiles/l/8/12/32", nil)
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

	etcdReq := testcontainers.ContainerRequest{
		Image: "openeuler/etcd:latest",
		WaitingFor: wait.ForAll(
			wait.ForLog("ready to serve client requests"),
			wait.ForListeningPort("2379/tcp"),
		),
		ExposedPorts: []string{"2379"},
		Env: map[string]string{
			"ALLOW_NONE_AUTHENTICATION":  "yes",
			"ETCD_LISTEN_CLIENT_URLS":    "http://0.0.0.0:2379",
			"ETCD_ADVERTISE_CLIENT_URLS": "http://etcd-server:2379",
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
		if err == nil {
			endpoint, err = etcdC.Endpoint(ctx, "")
		}
		if err == nil {
			break
		}
		if etcdC != nil {
			_ = etcdC.Terminate(ctx)
			etcdC = nil
		}
		time.Sleep(3 * time.Second)
	}

	require.NoError(t, err)

	defer func() {
		require.NoError(t, etcdC.Terminate(ctx))
	}()

	port := testutil.FreePort(t)

	cfg := fmt.Sprintf(`server:
  port: %d
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`, port)

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

	var execErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		execErr = rootCmd.Execute()
	}()

	url := fmt.Sprintf("http://localhost:%d/tiles/color/8/12/32", port)

	var resp *http.Response

	defer func() {
		select {
		case <-done:
		default:
			// Signalling after exit would hit no handler and kill the test binary
			require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR1))
			<-done
		}
		if resp != nil {
			resp.Body.Close()
		}
		assert.NoError(t, execErr)
	}()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
		}

		return tileStatus(url) != 0
	}, 15*time.Second, 100*time.Millisecond)

	select {
	case <-done:
		require.Fail(t, "server exited before serving", "%v", execErr)
	default:
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	if assert.NotNil(t, resp) {
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	}
}
