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

package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_LogFileWriterReturnsCloser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")

	out, closeLog, err := makeLogFileWriter(path, false)
	require.NoError(t, err)
	require.NotNil(t, closeLog)

	_, err = out.Write([]byte("entry\n"))
	require.NoError(t, err)

	require.NoError(t, closeLog())

	content, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	assert.Contains(t, string(content), "entry")
}

func Test_AccessLoggingToStdoutNeedsNoClose(t *testing.T) {
	cfg := config.AccessConfig{Console: true, Format: config.AccessFormatCommon}

	_, closeLog, err := configureAccessLogging(cfg, config.DefaultConfig().Error.Messages, http.NotFoundHandler())
	require.NoError(t, err)
	require.NotNil(t, closeLog, "callers must never nil-check the returned closer")

	// Closing stdout would break logging for the rest of the process.
	require.NoError(t, closeLog())
}

func Test_ConfigureAccessLoggingClosesFileOnFormatError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	cfg := config.AccessConfig{Path: path, Console: false, Format: "invalid-format"}

	_, closeLog, err := configureAccessLogging(cfg, config.DefaultConfig().Error.Messages, http.NotFoundHandler())
	require.Error(t, err)
	require.NotNil(t, closeLog, "must return non-nil closer even on error")

	// Validation happens before file opening, so file should not exist.
	_, statErr := os.Stat(path)
	require.Error(t, statErr, "file should not exist since validation happens before open")

	// Verify the closer does not panic and is callable.
	require.NoError(t, closeLog())
}

func Test_ConfigureMainLoggingClosesFileOnFormatError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.log")
	cfg := config.DefaultConfig()
	cfg.Logging.Main.Path = path
	cfg.Logging.Main.Format = "invalid-format"
	cfg.Logging.Main.Level = "info"

	_, err := configureMainLogging(&cfg)
	require.Error(t, err)

	// Validation happens before file opening, so file should not exist.
	_, statErr := os.Stat(path)
	assert.Error(t, statErr, "file should not exist since validation happens before open")
}

func Test_ConfigureMainLoggingClosesFileOnLevelError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.log")
	cfg := config.DefaultConfig()
	cfg.Logging.Main.Path = path
	cfg.Logging.Main.Format = config.MainFormatPlain
	cfg.Logging.Main.Level = "invalid-level"

	_, err := configureMainLogging(&cfg)
	require.Error(t, err)

	// The file should not have been opened since level is validated first.
	_, statErr := os.Stat(path)
	assert.Error(t, statErr, "file should not exist since validation happens before open")
}
