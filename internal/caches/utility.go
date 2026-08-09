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

package caches

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
)

// Utility type used in a couple caches
type HostAndPort struct {
	Host string
	Port uint16
}

func (hp HostAndPort) String() string {
	return hp.Host + ":" + strconv.Itoa(int(hp.Port))
}

func HostAndPortArrayToStringArray(servers []HostAndPort) []string {
	addrs := make([]string, len(servers))

	for i, addr := range servers {
		addrs[i] = addr.String()
	}

	return addrs
}

// unsafeChar matches any character not in the safe set: ASCII letters, digits, hyphen, and period.
var unsafeChar = regexp.MustCompile(`[^A-Za-z0-9\-.]`)

// dotRun matches a run of two or more periods, e.g. "..", "...", etc.
var dotRun = regexp.MustCompile(`\.{2,}`)

// safeLayerName sanitizes a tile request's LayerName for use as (part of) a cache key/filename/
// object key. LayerName is attacker-controlled for pattern layers (it comes from the request
// path), so it can't be used verbatim: a value like "../../escaped" used to let a request escape
// the configured cache directory on disk, smuggle "/" into an S3 object key producing an
// unexpected key hierarchy, or otherwise violate a backend's key charset/length constraints.
//
// Unlike hashing the whole key, this preserves the human-readable structure of the original key
// (needed for debugging, S3 lifecycle rules/prefix-scoped IAM policies, etc.) by only replacing
// the unsafe characters within LayerName rather than replacing the whole key with an opaque
// digest.
//
// Every character outside the safe set (ASCII letters, digits, '-', and '.') is replaced with a
// single underscore. A literal "." is safe on its own, but a run of two or more consecutive dots
// is also replaced with underscores so that traversal segments like ".." (or "...", "....", etc.)
// can never reappear after substitution - e.g. "../../escaped" becomes "_.._.._escaped" ->
// "___/___/escaped" is never produced; concretely it becomes "_/_/escaped" component-wise before
// callers strip/replace the slashes too, since "/" itself is also outside the safe set.
//
// Collision note: this is a many-to-one mapping. Two distinct layer names that differ only in
// unsafe characters can sanitize to the same value and will then share cache entries - e.g. both
// "a/b" and "a b" sanitize to "a_b". This is an accepted tradeoff for keeping keys readable;
// callers that need strict uniqueness should not rely on LayerName alone.
func safeLayerName(name string) string {
	replaced := unsafeChar.ReplaceAllString(name, "_")
	replaced = dotRun.ReplaceAllString(replaced, "_")

	// A lone "." or ".." (already caught above, but guard the single-dot case explicitly) as the
	// entire name would otherwise be a valid "safe" string yet still resolve to the current or
	// parent directory when used as a path component.
	if replaced == "." {
		replaced = "_"
	}

	return replaced
}

// memcacheMaxKeyLength is memcache's hard key length limit, in bytes.
const memcacheMaxKeyLength = 250

// hashSuffixLength is the number of hex characters of a SHA-256 digest kept when a memcache key
// needs to be shortened to fit within the length limit.
const hashSuffixLength = 16

// safeMemcacheKey builds a memcache key from a prefix and an already-shaped key body (e.g.
// KeyPrefix + t.String() with the layer name already sanitized by safeLayerName), and guards
// against memcache's 250-byte key length limit. Sanitization alone doesn't shorten a key - a long
// but otherwise "safe" layer name can still overflow the limit - so if the combined key is too
// long, it's truncated and a short hash suffix (derived from the full original key) is appended
// to keep it unique. This is a documented fallback to hashing, only used in the rare case the
// human-readable key itself is too long; normal, reasonably-sized keys are left untouched.
func safeMemcacheKey(prefix, body string) string {
	key := prefix + body

	if len(key) <= memcacheMaxKeyLength {
		return key
	}

	sum := sha256.Sum256([]byte(key))
	suffix := "_" + hex.EncodeToString(sum[:])[:hashSuffixLength]

	// The prefix is configuration, not attacker-controlled, but an operator could still set one
	// long enough that prefix+suffix alone exceeds the limit. Truncate the prefix too in that
	// case so the result is always within bounds.
	maxPrefixLen := len(prefix)
	if maxPrefixLen > memcacheMaxKeyLength-len(suffix) {
		maxPrefixLen = memcacheMaxKeyLength - len(suffix)
	}
	if maxPrefixLen < 0 {
		maxPrefixLen = 0
	}
	truncatedPrefix := prefix[:maxPrefixLen]

	maxBodyLen := memcacheMaxKeyLength - len(truncatedPrefix) - len(suffix)
	if maxBodyLen < 0 {
		maxBodyLen = 0
	}
	if maxBodyLen > len(body) {
		maxBodyLen = len(body)
	}

	return truncatedPrefix + body[:maxBodyLen] + suffix
}
