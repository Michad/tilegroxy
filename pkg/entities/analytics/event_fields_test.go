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

package analytics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRequestContext(t *testing.T) context.Context {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "http://example.com/tiles/main/1/2/3?foo=bar", nil)
	req.RemoteAddr = "10.1.2.3:54321"
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("Referer", "http://example.com/map")
	req.Header.Set("X-Tenant-Id", "acme")

	return pkg.NewRequestContext(req)
}

func Test_FieldResolver_RejectsUnknownField(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	_, err := newFieldResolver(map[string]interface{}{"fields": []string{"notreal"}}, msgs)
	require.Error(t, err, "an unrecognized field name should fail at startup rather than silently yield nothing")
	assert.Contains(t, err.Error(), "notreal")
}

func Test_FieldResolver_AcceptsAllKnownFields(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	r, err := newFieldResolver(map[string]interface{}{"fields": AllFields}, msgs)
	require.NoError(t, err)

	ctx := testRequestContext(t)
	out := r.Resolve(ctx, FieldSource{LayerName: "main", Bytes: 42, ContentType: "image/png"})

	// Every documented field must resolve to something; a name that silently produces nothing is a
	// documentation bug waiting to happen.
	for _, f := range AllFields {
		assert.Contains(t, out, f, "field %v should resolve", f)
	}

	assert.Equal(t, "main", out[FieldLayerName])
	assert.Equal(t, 42, out[FieldBytes])
	assert.Equal(t, "image/png", out[FieldContentType])
	assert.Equal(t, "10.1.2.3", out[FieldIP])
	assert.Equal(t, "test-agent", out[FieldUserAgent])
	assert.Equal(t, "http://example.com/map", out[FieldReferer])
	assert.Equal(t, http.MethodGet, out[FieldMethod])
}

func Test_FieldResolver_CaseInsensitiveFieldNames(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	// Config keys are case insensitive elsewhere in tilegroxy, so values here should be too.
	r, err := newFieldResolver(map[string]interface{}{"fields": []string{"ContentType", "IP"}}, msgs)
	require.NoError(t, err)

	out := r.Resolve(testRequestContext(t), FieldSource{ContentType: "image/png"})

	assert.Equal(t, "image/png", out[FieldContentType])
	assert.Equal(t, "10.1.2.3", out[FieldIP])
}

func Test_FieldResolver_ExtraFields(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	r, err := newFieldResolver(map[string]interface{}{
		"extrafields": map[string]string{
			"tenant":       "hdr.X-Tenant-Id",
			"lowercasehdr": "hdr.x-tenant-id",
			"clientip":     "ctx.ip",
			"environment":  "production",
			"missing":      "hdr.X-Not-Present",
		},
	}, msgs)
	require.NoError(t, err)

	out := r.Resolve(testRequestContext(t), FieldSource{})

	assert.Equal(t, "acme", out["tenant"])
	assert.Equal(t, "acme", out["lowercasehdr"], "header lookups should be case insensitive")
	assert.Equal(t, "10.1.2.3", out["clientip"])
	assert.Equal(t, "production", out["environment"], "a value with no recognized prefix is a literal")
	assert.NotContains(t, out, "missing", "an absent source should be omitted rather than recorded as nil")
}

func Test_FieldResolver_EmptyConfigResolvesToNil(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	r, err := newFieldResolver(map[string]interface{}{}, msgs)
	require.NoError(t, err)

	assert.Nil(t, r.Resolve(testRequestContext(t), FieldSource{}))
}

func Test_FieldResolver_BackgroundContextDoesNotPanic(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	r, err := newFieldResolver(map[string]interface{}{"fields": AllFields}, msgs)
	require.NoError(t, err)

	// Seeding and health checks run against a synthetic context with no real request behind it.
	assert.NotPanics(t, func() {
		r.Resolve(pkg.BackgroundContext(), FieldSource{})
	})
}

func Test_FieldResolver_NilReceiver(t *testing.T) {
	var r *fieldResolver

	assert.Nil(t, r.Resolve(pkg.BackgroundContext(), FieldSource{}))
}
