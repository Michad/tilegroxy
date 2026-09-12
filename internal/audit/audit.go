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

// Package audit emits the security-relevant event stream: auth/authz failures and configuration reloads.
package audit

import (
	"context"
	"log/slog"

	"github.com/Michad/tilegroxy/pkg"
)

// Event types. These are stable identifiers operators match on in their log pipeline, so treat a
// change to one as a breaking change.
const (
	EventAuthFailure  = "auth_failure"
	EventConfigReload = "config_reload"
)

// Outcomes recorded on a config reload event.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
)

var logger *slog.Logger

func SetAuditLoggerOnStartup(l *slog.Logger) {
	logger = l
}

func Enabled() bool {
	return logger != nil
}

func log(ctx context.Context, msg string, attrs ...any) {
	if logger == nil {
		return
	}

	logger.InfoContext(ctx, msg, attrs...)
}

func AuthFailure(ctx context.Context, reason string) {
	if !Enabled() {
		return
	}

	attrs := []any{
		slog.String("event", EventAuthFailure),
		slog.String("reason", reason),
	}

	// The user only resolves when auth got far enough to identify someone
	if u, ok := pkg.UserIDFromContext(ctx); ok && u != nil && *u != "" {
		attrs = append(attrs, slog.String("user", *u))
	}

	if t, ok := pkg.TenantIDFromContext(ctx); ok && t != nil && *t != "" {
		attrs = append(attrs, slog.String("tenant", *t))
	}

	log(ctx, "Authentication failure", attrs...)
}

// ConfigReload records a hot reload of the configuration. A reload that fails to apply is recorded
// with the error that stopped it.
func ConfigReload(ctx context.Context, err error) {
	if !Enabled() {
		return
	}

	if err != nil {
		log(ctx, "Configuration reload failed",
			slog.String("event", EventConfigReload),
			slog.String("outcome", OutcomeFailure),
			slog.String("error", err.Error()))

		return
	}

	log(ctx, "Configuration reloaded",
		slog.String("event", EventConfigReload),
		slog.String("outcome", OutcomeSuccess))
}
