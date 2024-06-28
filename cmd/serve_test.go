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
	"fmt"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func coreServeTest(t *testing.T, cfg string, url string) (*http.Response, error, func()) {
	exitStatus = -1
	rootCmd.ResetFlags()
	seedCmd.ResetFlags()
	initRoot()
	initSeed()

	rootCmd.SetArgs([]string{"serve", "--raw-config", cfg})

	//This isn't proper goroutine practice but done this way since we only care about errors that happen at startup of the server
	var bindErr error

	go func() { bindErr = rootCmd.Execute() }()

	time.Sleep(1 * time.Second)

	if bindErr != nil {
		return nil, bindErr, nil
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err, nil
	}

	resp, err := http.DefaultClient.Do(req)

	return resp, err, func() {
		syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
		if resp != nil {
			resp.Body.Close()
		}
	}
}

func Test_ServeCommand_Execute(t *testing.T) {

	cfg := `server:
  port: 12342
  StaticHeaders:
    X-Test: result
  RootPath: "/root"
  TilePath: "/tiles"
  Production: false
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12342/root/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 200, resp.StatusCode)

	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	assert.Equal(t, "result", resp.Header["X-Test"][0])
	assert.Equal(t, "tilegroxy v0.X.Y", resp.Header["X-Powered-By"][0])
}

func Test_ServeCommand_ExecuteProduction(t *testing.T) {
	cfg := `server:
  port: 12343
  Production: true
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`
	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12343/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Nil(t, resp.Header["X-Powered-By"])
}

func Test_ServeCommand_ExecuteErrorImage(t *testing.T) {
	cfg := `server:
  port: 12344
Error:
  mode: "image"
authentication:
  name: static key
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12344/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	assert.Nil(t, resp.Header["X-Error-Message"])
}

func Test_ServeCommand_ExecuteErrorImageHeader(t *testing.T) {
	cfg := `server:
  port: 12345
Error:
  mode: "image+header"
authentication:
  name: static key
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`
	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12345/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	assert.NotNil(t, resp.Header["X-Error-Message"])
}

func Test_ServeCommand_ExecuteErrorText(t *testing.T) {
	cfg := `server:
  port: 12346
Error:
  mode: "text"
authentication:
  name: static key
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`
	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12346/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, "text/plain; charset=utf-8", resp.Header["Content-Type"][0])
	assert.Nil(t, resp.Header["X-Error-Message"])
}

func Test_ServeCommand_ExecuteStatic(t *testing.T) {

	cfg := `server:
  port: 12347
Authentication:
  name: static key
  key: hunter2
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12347/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)

	req, err := http.NewRequest(http.MethodGet, "http://localhost:12347/tiles/color/8/12/32", nil)
	req.Header.Add("Authorization", "Bearer hunter2")
	assert.Nil(t, err)

	resp2, err := http.DefaultClient.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp2.StatusCode)

	resp2.Body.Close()
}

// Just make sure it starts up and rejects unauth for now. TODO: figure out how to get the key from logs
func Test_ServeCommand_ExecuteStaticRandomKey(t *testing.T) {

	cfg := `server:
  port: 12348
Authentication:
  name: static key
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12348/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)
}

func Test_ServeCommand_ExecuteJWT_MissingAlg(t *testing.T) {
	cfg := `server:
  port: 12349
Authentication:
  name: jwt
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12349/tiles/color/8/12/32")
	if postFunc != nil {
		defer postFunc()
	}

	assert.NotNil(t, err)
	assert.Nil(t, resp)
}

func Test_ServeCommand_ExecuteJWT(t *testing.T) {
	cfg := `server:
  port: 12349
Authentication:
  name: jwt
  Algorithm: HS256
  VerificationKey: hunter2
  MaxExpirationDuration: 4294967295
  ExpectedAudience: audience
  ExpectedSubject: subject
  ExpectedIssuer: issuer
  ExpectedScope: tile
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`

	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12349/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)

	req, err := http.NewRequest(http.MethodGet, "http://localhost:12349/tiles/color/8/12/32", nil)
	req.Header.Add("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzdWJqZWN0IiwiYXVkIjoiYXVkaWVuY2UiLCJpc3MiOiJpc3N1ZXIiLCJzY29wZSI6InNvbWV0aGluZyB0aWxlIG90aGVyIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyLCJleHAiOjQyOTQ5NjcyOTV9.6jOBwjsvFcJXGkaleXB-75F6J3CjaQYuRELJPfvOfQE")
	assert.Nil(t, err)

	resp2, err := http.DefaultClient.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp2.StatusCode)

	resp2.Body.Close()
}

func Test_ServeCommand_ExecuteCustom(t *testing.T) {
	cfg := `server:
  port: 12341
Authentication:
  name: custom
  token:
    header: X-Token
  script: |
    package custom
    import (
    	"os"
    	"time"
    )
    func validate(token string) (bool, time.Time, string, []string) {
    	if string(token) == "hunter2" {
    		return true, time.Now().Add(1 * time.Hour), "user", []string{"color"}
    	}
    	return false, time.Now().Add(1000 * time.Hour), "", []string{}
    }
layers:
  - id: color2
    skipCache: true
    provider:
      name: static
      color: "FFFFFF"
  - id: color
    skipCache: true
    provider:
      name: static
      color: "FFFFFF"
`
	fmt.Println(cfg)

	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12341/tiles/color/8/12/32")
	defer postFunc()

	if err != nil {
		fmt.Println(err.Error())
	}

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)

	req, err := http.NewRequest(http.MethodGet, "http://localhost:12341/tiles/color/8/12/32", nil)
	req.Header.Add("X-Token", "hunter2")
	assert.Nil(t, err)

	resp2, err := http.DefaultClient.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp2.StatusCode)

	resp2.Body.Close()

	req, err = http.NewRequest(http.MethodGet, "http://localhost:12341/tiles/color2/8/12/32", nil)
	req.Header.Add("X-Token", "hunter2")
	assert.Nil(t, err)

	resp3, err := http.DefaultClient.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 401, resp2.StatusCode)

	resp3.Body.Close()
}
