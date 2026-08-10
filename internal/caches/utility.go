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

var unsafeChar = regexp.MustCompile(`[^A-Za-z0-9\-.]`)

// Dots are safe individually but a run of them is a traversal segment.
var dotRun = regexp.MustCompile(`\.{2,}`)

// safeLayerName sanitizes a tile request's LayerName for use as part of a cache key, filename, or
// object key. LayerName is User input for pattern layers, so it can't be used verbatim: it could
// otherwise escape the disk cache directory, smuggle "/" into an S3 object key, or violate a
// backend's key charset limits.
//
// Substituting rather than hashing keeps keys human-readable, which S3 lifecycle rules and
// prefix-scoped IAM policies depend on. The tradeoff is that the mapping is many-to-one: "a/b" and
// "a b" both become "a_b" and then share cache entries.
func safeLayerName(name string) string {
	replaced := unsafeChar.ReplaceAllString(name, "_")
	replaced = dotRun.ReplaceAllString(replaced, "_")

	// A name that is only "." still resolves to a directory when used as a path component.
	if replaced == "." {
		replaced = "_"
	}

	return replaced
}

// memcache's hard key length limit, in bytes.
const memcacheMaxKeyLength = 250

const hashSuffixLength = 16

// safeMemcacheKey builds a memcache key from a prefix and an already-sanitized body. A long but
// otherwise safe layer name can still overflow the length limit, so an oversized key is truncated
// with a hash suffix appended to keep it unique.
func safeMemcacheKey(prefix, body string) string {
	key := prefix + body

	if len(key) <= memcacheMaxKeyLength {
		return key
	}

	sum := sha256.Sum256([]byte(key))
	suffix := "_" + hex.EncodeToString(sum[:])[:hashSuffixLength]

	// An operator could set a prefix long enough that prefix+suffix alone exceeds the limit.
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
