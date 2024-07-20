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

import "fmt"

type AuthError struct {
	Message string
}

func (e AuthError) Error() string {
	// notest
	return fmt.Sprintf("Auth Error - %s", e.Message)
}

type InvalidContentLengthError struct {
	Length int
}

func (e *InvalidContentLengthError) Error() string {
	// notest
	return fmt.Sprintf("Invalid content length %v", e.Length)
}

type InvalidContentTypeError struct {
	ContentType string
}

func (e *InvalidContentTypeError) Error() string {
	// notest
	return fmt.Sprintf("Invalid content type %v", e.ContentType)
}

type RemoteServerError struct {
	StatusCode int
}

func (e *RemoteServerError) Error() string {
	// notest
	return fmt.Sprintf("Remote server returned status code %v", e.StatusCode)
}
