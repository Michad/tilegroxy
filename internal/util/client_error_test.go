// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	neturl "net/url"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

const secretURL = "https://user:pass@secret-host.example/tiles/1/2/3.png?key=SECRETKEY&style=dark"

type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string { return "secret-host.example i/o timeout" }
func (fakeTimeoutError) Timeout() bool { return true }

func Test_DescribeClientError(t *testing.T) {
	addr := &net.TCPAddr{IP: net.IPv4(10, 1, 2, 3), Port: 443}

	tests := []struct {
		name     string
		inner    error
		cause    string
		timeout  bool
		canceled bool
	}{
		{
			name:  "refused",
			inner: &net.OpError{Op: "dial", Net: "tcp", Addr: addr, Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}},
			cause: "dial tcp: connection refused",
		},
		{
			name:  "op without errno",
			inner: &net.OpError{Op: "read", Net: "tcp", Addr: addr, Err: errors.New("secret-host.example broke")},
			cause: "read tcp: *errors.errorString",
		},
		{
			name:  "dns not found",
			inner: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Name: "secret-host.example", Err: "no such host", IsNotFound: true}},
			cause: "DNS lookup failed: no such host",
		},
		{
			name:  "dns other",
			inner: &net.DNSError{Name: "secret-host.example", Err: "server misbehaving"},
			cause: "DNS lookup failed",
		},
		{
			name:    "deadline",
			inner:   context.DeadlineExceeded,
			cause:   "context deadline exceeded",
			timeout: true,
		},
		{
			name:     "canceled",
			inner:    context.Canceled,
			cause:    "context canceled",
			canceled: true,
		},
		{
			name:  "eof",
			inner: io.EOF,
			cause: "connection closed by remote server",
		},
		{
			name:  "tls",
			inner: x509.HostnameError{Host: "secret-host.example"},
			cause: "x509.HostnameError",
		},
		{
			name:    "other timeout",
			inner:   fakeTimeoutError{},
			cause:   "util.fakeTimeoutError",
			timeout: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := DescribeClientError(&neturl.Error{Op: "Get", URL: secretURL, Err: tt.inner})

			assert.Equal(t, tt.cause, failure.Cause)
			assert.Equal(t, tt.timeout, failure.Timeout)
			assert.Equal(t, tt.canceled, failure.Canceled)
			assert.NotContains(t, failure.Cause, "secret-host")
			assert.NotContains(t, failure.Cause, "10.1.2.3")
			assert.NotContains(t, failure.Detail, "SECRETKEY")
			assert.NotContains(t, failure.Detail, "pass")
			assert.Contains(t, failure.Detail, "style=dark")
		})
	}
}

func Test_DescribeClientError_NotURLError(t *testing.T) {
	failure := DescribeClientError(errors.New("boom"))

	assert.Equal(t, "*errors.errorString", failure.Cause)
	assert.Equal(t, "boom", failure.Detail)
	assert.False(t, failure.Timeout)
}
