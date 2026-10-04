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

//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

// Permissions for the generated config file, which only the test process needs to read.
const configFileMode = 0600

// Config describes an instance to start. Raw is YAML which may reference {{.Port}} and
// {{.HealthPort}}; the harness allocates both and substitutes them.
type Config struct {
	Raw       string
	Args      []string
	Env       []string
	HotReload bool
}

type ports struct {
	Server int
	Health int
}

func renderConfig(t *testing.T, raw string, p ports) string {
	t.Helper()

	tmpl, err := template.New("config").Parse(raw)
	if err != nil {
		t.Fatalf("config template is not valid: %v", err)
	}

	// The template names the ports as the config keys spell them, not as the struct fields do.
	data := struct {
		Port       int
		HealthPort int
	}{Port: p.Server, HealthPort: p.Health}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		t.Fatalf("cannot render config template: %v", err)
	}

	return sb.String()
}

func writeConfig(t *testing.T, rendered string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "tilegroxy.yml")

	if err := os.WriteFile(path, []byte(rendered), configFileMode); err != nil {
		t.Fatalf("cannot write config: %v", err)
	}

	return path
}
