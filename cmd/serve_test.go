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
	go func() { assert.Nil(t, rootCmd.Execute()) }()

	time.Sleep(1 * time.Second)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	assert.Nil(t, err)

	resp, err := http.DefaultClient.Do(req)

	return resp, err, func() {
		syscall.Kill(syscall.Getpid(), syscall.SIGINT)
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
  port: 12342
  Production: true
layers:
  - id: color
    provider:
      name: static
      color: "FFFFFF"
`
	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12342/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 200, resp.StatusCode)
	assert.Nil(t, resp.Header["X-Powered-By"])
}

func Test_ServeCommand_ExecuteErrorImage(t *testing.T) {
	cfg := `server:
  port: 12342
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

	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12342/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	assert.Nil(t, resp.Header["X-Error-Message"])
}

func Test_ServeCommand_ExecuteErrorImageHeader(t *testing.T) {
	cfg := `server:
  port: 12342
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
	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12342/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header["Content-Type"][0])
	assert.NotNil(t, resp.Header["X-Error-Message"])
}

func Test_ServeCommand_ExecuteErrorText(t *testing.T) {
	cfg := `server:
  port: 12342
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
	resp, err, postFunc := coreServeTest(t, cfg, "http://localhost:12342/tiles/color/8/12/32")
	defer postFunc()

	assert.Nil(t, err)
	assert.NotNil(t, resp)

	assert.Equal(t, 401, resp.StatusCode)
	assert.Equal(t, "text/plain; charset=utf-8", resp.Header["Content-Type"][0])
	assert.Nil(t, resp.Header["X-Error-Message"])
}
