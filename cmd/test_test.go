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

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ExecuteTestCommandNoCache(t *testing.T) {
	rootCmd.ResetFlags()
	testCmd.ResetFlags()
	initRoot()
	initTest()
	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"test", "-c", "../examples/configurations/simple.json", "--no-cache"})
	assert.Nil(t, cmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.Greater(t, len(out), 10)
	assert.Less(t, exitStatus, 1)
}

func Test_ExecuteTestCommand(t *testing.T) {
	rootCmd.ResetFlags()
	testCmd.ResetFlags()
	initRoot()
	initTest()

	cmd := rootCmd
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	cmd.SetArgs([]string{"test", "-c", "../examples/configurations/simple.json"})
	assert.Nil(t, cmd.Execute())
	out, err := io.ReadAll(b)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(out))

	assert.Greater(t, len(out), 10)
	assert.Equal(t, exitStatus, 1)
}
