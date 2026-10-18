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
	"errors"
	"fmt"
	"io"
	"net"
	neturl "net/url"
	"syscall"
)

// ClientFailure summarizes an http.Client error. The upstream URL can hold an operator's
// credentials, and the hostname can too, so Cause and Timeout never include either.
type ClientFailure struct {
	Cause    string
	Timeout  bool
	Canceled bool
	// The original message with the URL passed through RedactURLForLog. Only for debug logs.
	Detail string
}

// DescribeClientError is meant for errors from http.Client.Do, which are always a *url.Error.
func DescribeClientError(err error) ClientFailure {
	failure := ClientFailure{
		Cause:    clientErrorCause(err),
		Canceled: errors.Is(err, context.Canceled),
		Detail:   err.Error(),
	}

	var urlErr *neturl.Error
	if errors.As(err, &urlErr) {
		redacted := *urlErr
		redacted.URL = RedactURLForLog(urlErr.URL)
		failure.Detail = redacted.Error()
		failure.Timeout = urlErr.Timeout()
		failure.Cause = clientErrorCause(urlErr.Err)
	}

	return failure
}

// Only static strings, type names and errno text are used: the messages of DNS, dial and TLS
// errors embed the hostname or address.
func clientErrorCause(err error) string {
	var dnsErr *net.DNSError
	var opErr *net.OpError
	var errno syscall.Errno

	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded.Error()
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return "DNS lookup failed: no such host"
		}
		return "DNS lookup failed"
	case errors.As(err, &opErr):
		if errors.As(opErr.Err, &errno) {
			return opErr.Op + " " + opErr.Net + ": " + errno.Error()
		}
		return fmt.Sprintf("%v %v: %T", opErr.Op, opErr.Net, opErr.Err)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection closed by remote server"
	}

	return fmt.Sprintf("%T", err)
}
