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

// Indicates high level categories of errors, used to decide the HTTP status code to return and the level of logging to use for such an error
type TypeOfError int

const (
	// Indicates an error with the geographic extent or tile coordinates requested. Generally a 400
	TypeOfErrorBounds = iota
	// Indicates an authentication related issue - note this is incoming auth, not outgoing auth. Generally a 401
	TypeOfErrorAuth
	// Indicates a provider did something unexpected. Maybe the API we're calling is down. Generally a 500
	TypeOfErrorProvider
	// Indicates something wrong with the incoming request besides what's covered in bounds
	TypeOfErrorBadRequest
	// Indicates something that doesn't fall into the above categories. This is usually a real problem that the operator needs to be aware of. Generally a 500
	TypeOfErrorOther
	// Indicates the requested layer doesn't exist, or the caller is authenticated but not authorized for
	// it (out of JWT LayerScope, area restriction, etc). Both cases return this rather than
	// TypeOfErrorAuth so an authenticated-but-restricted caller can't enumerate real layer names by
	// distinguishing "forbidden" from "never configured". Generally a 404
	TypeOfErrorNotFound
)

// The main interface for errors returned through the application. Indicates the type or category of the error and separates the error message that should be reported externally (with localization using the configurable error messages) from the internal error for logs (which uses the traditional Error() interface)
type TypedError interface {
	error
	Type() TypeOfError
	External(errorMessages config.ErrorMessages) string
}

// General error for incoming auth issues. Avoids returning specifics through the API so as not to help attackers.
// This is only for a caller who isn't authenticated at all (missing/invalid/expired credentials). A caller who
// authenticated successfully but isn't authorized for the specific layer requested should use NotFoundError instead,
// so the two cases can't be told apart by an authenticated-but-restricted caller probing layer names.
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

// Used both for a layer name that doesn't match any configured layer and for a layer that exists but the
// authenticated caller isn't authorized for (JWT LayerScope, area restriction, etc). Returning the same
// error/status for both prevents an authenticated-but-restricted caller from enumerating real layer names.
type NotFoundError struct {
	Message string
}

func (e NotFoundError) Error() string {
	// notest
	return fmt.Sprintf("Not Found - %v", e.Message)
}

func (e NotFoundError) Type() TypeOfError {
	// notest
	return TypeOfErrorNotFound
}

func (e NotFoundError) External(messages config.ErrorMessages) string {
	// notest
	return messages.NotFound
}

// The error used when a provider has an auth error. This special error is used by the application to indicate that a re-auth needs to occur. If the same error is passed back on that re-auth then it's treated as a normal error and returned back through API - therefore this is a provider error type, not an auth one
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

// Indicates the provider returned an unacceptable content length based on the configuration
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

// Indicates the provider returned an unacceptable content type based on the configuration
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

// Indicates the provider returned an unacceptable status code based on the configuration
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

// Indicates an input from the user is outside the valid range allowed for a numeric parameter - primarily tile coordinates
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

// Indicates too many tiles will be returned for a given request than the system can safely handle
type TooManyTilesError struct {
	NumTiles uint64
}

func (e TooManyTilesError) Error() string {
	// notest
	return fmt.Sprintf("too many tiles to return (%v > 10000)", e.NumTiles)
}

func (e TooManyTilesError) Type() TypeOfError {
	// notest
	return TypeOfErrorBadRequest
}

func (e TooManyTilesError) External(messages config.ErrorMessages) string {
	// notest
	return fmt.Sprintf(messages.RangeError, "tile count", 1, e.NumTiles)
}

// Indicates generally a bad input from the user
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
