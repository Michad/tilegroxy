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

package pkg

import (
	"fmt"

	"github.com/Michad/tilegroxy/pkg/config"
)

// Decides the HTTP status code and log level for an error
type TypeOfError int

const (
	// Bad geographic extent or tile coordinates. Generally a 400
	TypeOfErrorBounds = iota
	// Incoming auth, not outgoing. Generally a 401
	TypeOfErrorAuth
	// A provider misbehaved, maybe because its upstream is down. Generally a 500
	TypeOfErrorProvider
	// Problems with the request besides bounds
	TypeOfErrorBadRequest
	// Usually a real problem the operator needs to know about. Generally a 500
	TypeOfErrorOther
	// Exceeded the configured server timeout. Generally a 503
	TypeOfErrorTimeout
)

// Separates the localized external message from the internal Error() text used for logs
type TypedError interface {
	error
	Type() TypeOfError
	External(errorMessages config.ErrorMessages) string
}

// Avoids returning specifics through the API so as not to help attackers
type UnauthorizedError struct {
	Message string
}

func (e UnauthorizedError) Error() string {
	// notest
	return fmt.Sprintf("Auth Error - %v", e.Message)
}

func (e UnauthorizedError) Type() TypeOfError {
	// notest
	return TypeOfErrorAuth
}

func (e UnauthorizedError) External(messages config.ErrorMessages) string {
	// notest
	return messages.NotAuthorized
}

// Triggers a re-auth. If re-auth returns it again it's a normal provider error, not an auth one
type ProviderAuthError struct {
	Message string
}

func (e ProviderAuthError) Error() string {
	// notest
	return "Provider Error - " + e.Message
}

func (e ProviderAuthError) Type() TypeOfError {
	// notest
	return TypeOfErrorProvider
}

func (e ProviderAuthError) External(_ config.ErrorMessages) string {
	// notest
	return e.Error()
}

// The provider returned a content length the configuration doesn't allow
type InvalidContentLengthError struct {
	Length int
}

func (e InvalidContentLengthError) Error() string {
	// notest
	return fmt.Sprintf("Invalid content length %v", e.Length)
}

func (e InvalidContentLengthError) Type() TypeOfError {
	// notest
	return TypeOfErrorProvider
}

func (e InvalidContentLengthError) External(messages config.ErrorMessages) string {
	// notest
	return messages.ProviderError
}

// The provider returned a content type the configuration doesn't allow
type InvalidContentTypeError struct {
	ContentType string
}

func (e InvalidContentTypeError) Error() string {
	// notest
	return fmt.Sprintf("Invalid content type %v", e.ContentType)
}

func (e InvalidContentTypeError) Type() TypeOfError {
	// notest
	return TypeOfErrorProvider
}

func (e InvalidContentTypeError) External(messages config.ErrorMessages) string {
	// notest
	return messages.ProviderError
}

// The provider returned a status code the configuration doesn't allow
type RemoteServerError struct {
	StatusCode int
}

func (e RemoteServerError) Error() string {
	// notest
	return fmt.Sprintf("Remote server returned status code %v", e.StatusCode)
}

func (e RemoteServerError) Type() TypeOfError {
	// notest
	return TypeOfErrorProvider
}

func (e RemoteServerError) External(messages config.ErrorMessages) string {
	// notest
	return messages.ProviderError
}

type InvalidSridError struct {
	srid uint
}

func (e InvalidSridError) Error() string {
	// notest
	return fmt.Sprintf("Supported projections only includes 4326 and 3857, not %v", e.srid)
}

func (e InvalidSridError) Type() TypeOfError {
	// notest
	return TypeOfErrorOther
}

func (e InvalidSridError) External(messages config.ErrorMessages) string {
	// notest
	return fmt.Sprintf(messages.EnumError, "provider.url template.srid", e.srid, []int{SRIDPsuedoMercator, SRIDWGS84})
}

// A numeric User input, primarily a tile coordinate, is outside its valid range
type RangeError struct {
	ParamName string
	MinValue  float64
	MaxValue  float64
}

func (e RangeError) Error() string {
	// notest
	return fmt.Sprintf("Param %v must be between %v and %v", e.ParamName, e.MinValue, e.MaxValue)
}

func (e RangeError) Type() TypeOfError {
	// notest
	return TypeOfErrorBounds
}

func (e RangeError) External(messages config.ErrorMessages) string {
	// notest
	return fmt.Sprintf(messages.RangeError, e.ParamName, e.MinValue, e.MaxValue)
}

// General bad input from the User
type InvalidArgumentError struct {
	Name  string
	Value any
}

func (e InvalidArgumentError) Error() string {
	// notest
	return fmt.Sprintf("%v cannot be %v", e.Name, e.Value)
}

func (e InvalidArgumentError) Type() TypeOfError {
	// notest
	return TypeOfErrorBadRequest
}

func (e InvalidArgumentError) External(messages config.ErrorMessages) string {
	// notest
	return fmt.Sprintf(messages.InvalidParam, e.Name, e.Value)
}

// The request ran longer than the configured server timeout
type TimeoutError struct{}

func (e TimeoutError) Error() string {
	// notest
	return "request exceeded the configured server timeout"
}

func (e TimeoutError) Type() TypeOfError {
	// notest
	return TypeOfErrorTimeout
}

func (e TimeoutError) External(messages config.ErrorMessages) string {
	// notest
	return messages.Timeout
}

// Indicates the request to a remote server failed without a response, such as a DNS failure, refused connection, or timeout.
// Never includes the URL or hostname since either can hold an operator's credentials.
type RemoteConnectionError struct {
	Cause   string
	Timeout bool
}

func (e RemoteConnectionError) Error() string {
	if e.Timeout {
		return "Remote server request timed out: " + e.Cause
	}

	return "Remote server request failed: " + e.Cause
}

func (e RemoteConnectionError) Type() TypeOfError {
	if e.Timeout {
		return TypeOfErrorTimeout
	}

	return TypeOfErrorProvider
}

func (e RemoteConnectionError) External(messages config.ErrorMessages) string {
	if e.Timeout {
		return messages.Timeout
	}

	return messages.ProviderError
}
