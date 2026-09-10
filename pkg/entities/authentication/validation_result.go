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

package authentication

import "time"

// ValidationResult is returned by a custom authentication script's validate function. Fields are
// added here rather than as new return values so adding a capability never requires another
// breaking change to the validate signature.
type ValidationResult struct {
	// Whether the token is valid and should allow the request to proceed
	Pass bool
	// When the authentication status of the token expires and validate should be called again.
	// Pass should be false for already-expired tokens
	Expiration time.Time
	// An identifier for the user being authenticated. By default this is only used for logging
	UserID string
	// An identifier for the tenant/organization the user belongs to. By default this is only
	// used for logging and analytics
	TenantID string
	// The specific layer IDs to allow access to with this token. Leave empty to allow all layers
	AllowedLayers []string
}
