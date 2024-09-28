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
	"context"
	"errors"
	"log/slog"
)

// Simple slog.Handler that sends all requests to children handlers
type MultiHandler struct {
	handlers []slog.Handler
}

func (h MultiHandler) Enabled(c context.Context, l slog.Level) bool {
	enabled := false

	for _, handle := range h.handlers {
		enabled = handle.Enabled(c, l)

		if enabled {
			break
		}
	}

	return enabled
}

func (h MultiHandler) Handle(c context.Context, r slog.Record) error {
	var err error

	for _, handle := range h.handlers {
		if handle.Enabled(c, r.Level) {
			err = errors.Join(handle.Handle(c, r))
		}
	}

	return err
}

func (h MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, 0, len(h.handlers))

	for _, handle := range h.handlers {
		newHandlers = append(newHandlers, handle.WithAttrs(attrs))
	}

	return MultiHandler{newHandlers}
}

func (h MultiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, 0, len(h.handlers))

	for _, handle := range h.handlers {
		newHandlers = append(newHandlers, handle.WithGroup(name))
	}

	return MultiHandler{newHandlers}
}
