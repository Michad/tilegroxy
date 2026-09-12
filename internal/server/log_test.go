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

package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Michad/tilegroxy/internal/audit"
	"github.com/Michad/tilegroxy/pkg"
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

	content, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
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

func Test_AuditLoggingDisabledByDefault(t *testing.T) {
	cfg := config.DefaultConfig()

	closeLog, err := configureAuditLogging(cfg.Logging.Audit, cfg.Error.Messages)
	require.NoError(t, err)
	require.NotNil(t, closeLog)
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	assert.False(t, audit.Enabled())
	require.NoError(t, closeLog())
}

func Test_AuditLoggingWritesEventsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	cfg := config.DefaultConfig()
	cfg.Logging.Audit.Enabled = true
	cfg.Logging.Audit.Console = false
	cfg.Logging.Audit.Path = path

	closeLog, err := configureAuditLogging(cfg.Logging.Audit, cfg.Error.Messages)
	require.NoError(t, err)
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	require.True(t, audit.Enabled())

	audit.ConfigReload(pkg.BackgroundContext(), nil)
	require.NoError(t, closeLog())

	content, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	require.NoError(t, err)
	assert.Contains(t, string(content), audit.EventConfigReload)
}

func Test_AuditLoggingIncludesRequestAttributes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	cfg := config.DefaultConfig()
	cfg.Logging.Audit.Enabled = true
	cfg.Logging.Audit.Console = false
	cfg.Logging.Audit.Path = path
	cfg.Logging.Audit.Headers = []string{"X-Correlation-Id"}

	closeLog, err := configureAuditLogging(cfg.Logging.Audit, cfg.Error.Messages)
	require.NoError(t, err)
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	req := httptest.NewRequest(http.MethodGet, "/tiles/osm/1/2/3", nil)
	req.Header.Set("X-Correlation-Id", "abc123")
	audit.AuthFailure(pkg.NewRequestContext(req), "CheckAuthentication returned false")

	require.NoError(t, closeLog())

	content, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	require.NoError(t, err)
	assert.Contains(t, string(content), "/tiles/osm/1/2/3")
	assert.Contains(t, string(content), "abc123")
}

// A config reload has no request behind it, so the request attributes must be left off rather than
// recorded as a row of empty strings.
func Test_AuditLoggingOmitsRequestAttributesWithoutARequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	cfg := config.DefaultConfig()
	cfg.Logging.Audit.Enabled = true
	cfg.Logging.Audit.Console = false
	cfg.Logging.Audit.Path = path

	closeLog, err := configureAuditLogging(cfg.Logging.Audit, cfg.Error.Messages)
	require.NoError(t, err)
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	audit.ConfigReload(pkg.BackgroundContext(), nil)
	require.NoError(t, closeLog())

	content, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	require.NoError(t, err)
	assert.NotContains(t, string(content), `"uri":""`)
	assert.NotContains(t, string(content), `"ip":""`)
}

func Test_AuditLoggingRejectsInvalidFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	cfg := config.DefaultConfig()
	cfg.Logging.Audit.Enabled = true
	cfg.Logging.Audit.Path = path
	cfg.Logging.Audit.Format = "invalid-format"

	closeLog, err := configureAuditLogging(cfg.Logging.Audit, cfg.Error.Messages)
	require.Error(t, err)
	require.NotNil(t, closeLog, "must return non-nil closer even on error")

	// Validation happens before file opening, so file should not exist.
	_, statErr := os.Stat(path)
	require.Error(t, statErr, "file should not exist since validation happens before open")
	require.NoError(t, closeLog())
}

// Auditing on with console off and no path would silently drop the trail the operator asked for.
func Test_AuditLoggingRejectsNoDestination(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Logging.Audit.Enabled = true
	cfg.Logging.Audit.Console = false

	closeLog, err := configureAuditLogging(cfg.Logging.Audit, cfg.Error.Messages)
	require.Error(t, err)
	require.NoError(t, closeLog())
}

func Test_AuditLoggingPlainFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	cfg := config.DefaultConfig()
	cfg.Logging.Audit.Enabled = true
	cfg.Logging.Audit.Console = false
	cfg.Logging.Audit.Path = path
	cfg.Logging.Audit.Format = config.AuditFormatPlain

	closeLog, err := configureAuditLogging(cfg.Logging.Audit, cfg.Error.Messages)
	require.NoError(t, err)
	t.Cleanup(func() { audit.SetAuditLoggerOnStartup(nil) })

	audit.ConfigReload(pkg.BackgroundContext(), errors.New("bad layer"))
	require.NoError(t, closeLog())

	content, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	require.NoError(t, err)
	assert.Contains(t, string(content), "event="+audit.EventConfigReload)
	assert.Contains(t, string(content), "outcome="+audit.OutcomeFailure)
}
