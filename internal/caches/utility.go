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

// Dots are safe individually but a run of them is a traversal segment
var dotRun = regexp.MustCompile(`\.{2,}`)

// LayerName is User input. Substituting rather than hashing keeps keys readable, but "a/b" and "a b" collide
func safeLayerName(name string) string {
	replaced := unsafeChar.ReplaceAllString(name, "_")
	replaced = dotRun.ReplaceAllString(replaced, "_")

	// "." alone still resolves to a directory as a path component
	if replaced == "." {
		replaced = "_"
	}

	return replaced
}

// In bytes
const memcachedMaxKeyLength = 250

const hashSuffixLength = 16

// Long but safe layer names can still overflow the limit, so oversized keys are truncated with a hash suffix
func safeMemcachedKey(prefix, body string) string {
	key := prefix + body

	if len(key) <= memcachedMaxKeyLength {
		return key
	}

	sum := sha256.Sum256([]byte(key))
	suffix := "_" + hex.EncodeToString(sum[:])[:hashSuffixLength]

	// An operator could set a prefix long enough that prefix plus suffix alone exceeds the limit
	maxPrefixLen := len(prefix)
	if maxPrefixLen > memcachedMaxKeyLength-len(suffix) {
		maxPrefixLen = memcachedMaxKeyLength - len(suffix)
	}
	if maxPrefixLen < 0 {
		maxPrefixLen = 0
	}
	truncatedPrefix := prefix[:maxPrefixLen]

	maxBodyLen := memcachedMaxKeyLength - len(truncatedPrefix) - len(suffix)
	if maxBodyLen < 0 {
		maxBodyLen = 0
	}
	if maxBodyLen > len(body) {
		maxBodyLen = len(body)
	}

	return truncatedPrefix + body[:maxBodyLen] + suffix
}
