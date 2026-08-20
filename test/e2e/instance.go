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

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	// How long Start waits for the child to bind its port.
	startupTimeout = 30 * time.Second
	// How long a probe connection may take before it counts as not-yet-listening.
	probeTimeout = time.Second
	// How long test cleanup gives a child to exit after SIGTERM before killing it.
	cleanupTimeout = 15 * time.Second
)

// Instance is a running tilegroxy child process. Everything a test asserts against goes through
// here, so no test manipulates the process directly.
type Instance struct {
	ConfigPath string

	t      *testing.T
	cmd    *exec.Cmd
	cancel context.CancelFunc
	ports  ports
	out    *lockedBuffer
	waited bool
	code   int
	mu     sync.Mutex
}

// lockedBuffer collects child output. The process writes from its own goroutine while tests read,
// so the buffer needs its own lock.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.String()
}

// Start launches the binary with a generated config on freshly allocated ports and blocks until it
// answers, so tests never race against startup.
func Start(t *testing.T, c Config) *Instance {
	t.Helper()

	p := ports{Server: freePort(t), Health: freePort(t)}
	path := writeConfig(t, renderConfig(t, c.Raw, p))

	args := []string{"serve", "-c", path}
	if c.HotReload {
		args = append(args, "--hot-reload")
	}

	args = append(args, c.Args...)

	// The context is a backstop that outlives Start and is released by stop, so graceful shutdown
	// stays the harness's job rather than being pre-empted by a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, BinaryPath(t), args...)
	cmd.Env = append(os.Environ(), c.Env...)

	buf := &lockedBuffer{}
	cmd.Stdout = buf
	cmd.Stderr = buf

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("cannot start tilegroxy: %v", err)
	}

	inst := &Instance{ConfigPath: path, t: t, cmd: cmd, cancel: cancel, ports: p, out: buf}

	t.Cleanup(inst.stop)

	inst.waitReady()

	return inst
}

// waitReady polls the TCP port rather than an HTTP endpoint so it works whether or not the config
// enables health, and reports captured output on failure instead of a bare timeout.
func (i *Instance) waitReady() {
	i.t.Helper()

	deadline := time.Now().Add(Scale(startupTimeout))

	for time.Now().Before(deadline) {
		if !i.Running() {
			i.t.Fatalf("tilegroxy exited during startup. Output:\n%s", i.Output())
		}

		dialer := net.Dialer{Timeout: probeTimeout}

		conn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(i.ports.Server)))
		if err == nil {
			if err := conn.Close(); err != nil {
				i.t.Fatalf("cannot close probe connection: %v", err)
			}

			return
		}

		time.Sleep(pollInterval)
	}

	i.t.Fatalf("tilegroxy did not bind port %v in time. Output:\n%s", i.ports.Server, i.Output())
}

func (i *Instance) BaseURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(i.ports.Server)
}

func (i *Instance) HealthURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(i.ports.Health)
}

func (i *Instance) Output() string {
	return i.out.String()
}

// Running reports whether the child is still alive, without consuming its exit status. Signal 0 is
// the liveness probe; a nil signal is rejected by os.Process.Signal as an unsupported type.
func (i *Instance) Running() bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.waited {
		return false
	}

	return i.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (i *Instance) Signal(sig os.Signal) {
	i.t.Helper()

	i.mu.Lock()
	defer i.mu.Unlock()

	if i.waited {
		return
	}

	if err := i.cmd.Process.Signal(sig); err != nil {
		i.t.Fatalf("cannot signal tilegroxy: %v", err)
	}
}

// WaitExit blocks for the process to exit and returns its status code, failing the test if it
// outlives the timeout. Signals go to the child, so a hung shutdown fails one test rather than
// taking down the test binary.
func (i *Instance) WaitExit(timeout time.Duration) int {
	i.t.Helper()

	i.mu.Lock()
	if i.waited {
		defer i.mu.Unlock()
		return i.code
	}
	i.mu.Unlock()

	done := make(chan error, 1)

	go func() { done <- i.cmd.Wait() }()

	select {
	case err := <-done:
		i.mu.Lock()
		defer i.mu.Unlock()

		i.waited = true
		i.code = i.cmd.ProcessState.ExitCode()

		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			i.t.Fatalf("error waiting for tilegroxy: %v", err)
		}

		return i.code
	case <-time.After(Scale(timeout)):
		i.t.Fatalf("tilegroxy did not exit within %v. Output:\n%s", Scale(timeout), i.Output())
		return -1
	}
}

func (i *Instance) stop() {
	i.mu.Lock()
	alive := !i.waited
	i.mu.Unlock()

	if !alive {
		i.cancel()
		return
	}

	if i.t.Failed() {
		i.t.Logf("tilegroxy output:\n%s", i.Output())
	}

	_ = i.cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan struct{})

	go func() {
		_ = i.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(Scale(cleanupTimeout)):
		_ = i.cmd.Process.Kill()
		<-done
	}

	i.mu.Lock()
	i.waited = true
	i.mu.Unlock()

	i.cancel()
}

// Run executes a non-serve subcommand to completion and returns its combined output and exit code.
func Run(t *testing.T, args ...string) (string, int) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), BinaryPath(t), args...)

	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("cannot run tilegroxy %v: %v", args, err)
	}

	return string(out), cmd.ProcessState.ExitCode()
}
