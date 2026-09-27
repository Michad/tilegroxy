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

package configload

import "github.com/Michad/tilegroxy/pkg/config"

func MergeCacheControlDefaults(c *config.CacheControlConfig, other config.CacheControlConfig) {
	if c.Enabled == nil {
		c.Enabled = other.Enabled
	}

	if c.Auto == nil {
		c.Auto = other.Auto
	}

	if c.MaxAge == nil {
		c.MaxAge = other.MaxAge
	}

	if c.SharedMaxAge == nil {
		c.SharedMaxAge = other.SharedMaxAge
	}

	if c.StaleWhileRevalidate == nil {
		c.StaleWhileRevalidate = other.StaleWhileRevalidate
	}

	if c.Visibility == "" {
		c.Visibility = other.Visibility
	}

	if c.NoStore == nil {
		c.NoStore = other.NoStore
	}

	if len(c.Extra) == 0 {
		c.Extra = other.Extra
	}
}

func CacheControlEnabled(c config.CacheControlConfig) bool {
	return c.Enabled != nil && *c.Enabled
}

func CacheControlAutoEnabled(c config.CacheControlConfig) bool {
	return c.Auto == nil || *c.Auto
}

// EffectiveShutdownTimeout resolves the shutdown budget. When unset it covers both phases that
// consume it, the drain wait and a full-length request, so the budget is never smaller than the
// work it has to fit
func EffectiveShutdownTimeout(c config.ServerConfig) uint {
	if c.ShutdownTimeout == 0 {
		return c.Timeout + c.DrainDelay
	}

	return c.ShutdownTimeout
}

func MergeClientDefaults(c *config.ClientConfig, o config.ClientConfig) {
	if c.UserAgent == "" {
		c.UserAgent = o.UserAgent
	}
	if c.MaxLength == 0 {
		c.MaxLength = o.MaxLength
	}
	if c.UnknownLength == nil {
		c.UnknownLength = o.UnknownLength
	}
	if len(c.Headers) == 0 {
		c.Headers = o.Headers
	}
	if len(c.ContentTypes) == 0 {
		c.ContentTypes = o.ContentTypes
	}
	if len(c.StatusCodes) == 0 {
		c.StatusCodes = o.StatusCodes
	}
	if c.Timeout == 0 {
		c.Timeout = o.Timeout
	}
	if len(c.RewriteContentTypes) == 0 {
		c.RewriteContentTypes = o.RewriteContentTypes
	}
}
