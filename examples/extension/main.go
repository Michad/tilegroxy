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

// A custom tilegroxy executable. It registers the entities in ./sample, then checks every layer
// with the test command and starts serving, always using the tilegroxy.yml beside the binary.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Michad/tilegroxy/cmd"

	_ "example.com/my_tg_wrapper/sample"
)

func main() {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	configPath := filepath.Join(filepath.Dir(executable), "tilegroxy.yml")

	// A failing test exits the process, so serve only starts once every layer works
	cmd.ExecuteArgs("test", "-c", configPath)
	cmd.ExecuteArgs("serve", "-c", configPath)
}
