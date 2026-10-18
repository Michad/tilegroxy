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

// Returned by a custom auth script's validate. New fields avoid breaking the validate signature again
type ValidationResult struct {
	// Whether the request should proceed
	Pass bool
	// When validate should be called again. Pass should be false for already-expired tokens
	Expiration time.Time
	// By default only used for logging
	UserID string
	// The user's tenant or organization. By default only used for logging and analytics
	TenantID string
	// Layer IDs this token may access. Empty allows all layers
	AllowedLayers []string
}
