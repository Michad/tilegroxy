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

package tlsprofiles

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/Michad/tilegroxy/pkg/config"
)

const maxHostnameLength = 253

var hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

func normalizeAllowedHosts(entries []string, path string, messages config.ErrorMessages) ([]string, error) {
	result := make([]string, 0, len(entries))

	for _, entry := range entries {
		normalized := normalizeHost(entry)

		if !isHostOrIP(strings.TrimPrefix(normalized, "*.")) {
			return nil, fmt.Errorf(messages.InvalidParam, path+".allowedhosts", entry)
		}

		result = append(result, normalized)
	}

	return result, nil
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func isHostname(host string) bool {
	return len(host) <= maxHostnameLength && hostnamePattern.MatchString(host)
}

func isHostOrIP(host string) bool {
	return net.ParseIP(host) != nil || isHostname(host)
}

// A leading *. matches any number of subdomains, never the apex
func allowedHostMatches(entries []string, host string) bool {
	host = normalizeHost(host)

	for _, entry := range entries {
		if suffix, wildcard := strings.CutPrefix(entry, "*"); wildcard {
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		} else if entry == host {
			return true
		}
	}

	return false
}

// Certificate-style matching, where a wildcard covers exactly one label. Host is hostile input
func ServerHostMatches(hosts []string, host string) bool {
	host = normalizeHost(host)

	for _, entry := range hosts {
		if entry == host {
			return true
		}

		suffix, wildcard := strings.CutPrefix(entry, "*.")
		label, rest, found := strings.Cut(host, ".")

		if wildcard && found && rest == suffix && isHostname(label) {
			return true
		}
	}

	return false
}
