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

package placeholder

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSecreter map[string]string

func (s stubSecreter) Lookup(_ context.Context, key string) (string, string, error) {
	v, ok := s[key]
	if !ok {
		return "", "", errors.New("not found")
	}

	return v, "", nil
}

func (s stubSecreter) Check(_ context.Context, keys []string) ([]string, error) {
	return make([]string, len(keys)), nil
}

func secretResolver() *Resolver {
	return NewResolver(context.Background(), stubSecreter{"pw": "hunter2", "ref": "env.TEST_STATIC_A"})
}

func Test_ResolveString(t *testing.T) {
	t.Setenv("TEST_STATIC_A", "a")
	t.Setenv("TEST_STATIC_BRACES", "{env.TEST_STATIC_A}")

	tests := map[string]string{
		"env.TEST_STATIC_A":     "a",
		"env.TEST_STATIC_UNSET": "",
		"secret.pw":             "hunter2",
		"http://{env.TEST_STATIC_A}/x?k={secret.pw}": "http://a/x?k=hunter2",
		"{env.TEST_STATIC_UNSET:-fallback}":          "fallback",
		"{env.TEST_STATIC_A:-fallback}":              "a",
		"{secret.missing:-fallback}":                 "fallback",
		"{ctx.user}/{z}/{layer.v:-d}":                "{ctx.user}/{z}/{layer.v:-d}",
		"literal":                                    "literal",
		// Substituted values are never scanned again
		"{env.TEST_STATIC_BRACES}": "{env.TEST_STATIC_A}",
		"secret.ref":               "env.TEST_STATIC_A",
		// Whole value forms take the rest of the string as the name
		"env.TEST_STATIC_A/tiles": "",
	}

	for in, expected := range tests {
		t.Run(in, func(t *testing.T) {
			out, err := secretResolver().ResolveString(in)
			require.NoError(t, err)
			assert.Equal(t, expected, out)
		})
	}
}

func Test_ResolveString_SecretErrors(t *testing.T) {
	_, err := secretResolver().ResolveString("secret.missing")
	require.Error(t, err)

	_, err = secretResolver().ResolveString("a{secret.missing}")
	require.Error(t, err)

	_, err = NewResolver(context.Background(), nil).ResolveString("secret.pw")
	require.ErrorIs(t, err, errNoSecretStore)

	var nilResolver *Resolver
	_, err = nilResolver.ResolveString("{secret.pw}")
	require.ErrorIs(t, err, errNoSecretStore)

	out, err := nilResolver.ResolveString("{secret.pw:-d}")
	require.NoError(t, err)
	assert.Equal(t, "d", out)
}

type innerConfig struct {
	Value string
}

type namedString string

type walkConfig struct {
	Plain     string
	Named     namedString
	List      []string
	Headers   map[string]string
	Inner     innerConfig
	Pointer   *innerConfig
	NilPtr    *innerConfig
	Child     map[string]interface{}
	Any       any
	Template  Template
	Number    int
	Params    map[string]interface{} `mapstructure:",remain"`
	unexposed string
}

func Test_ResolveInto_Struct(t *testing.T) {
	t.Setenv("TEST_STATIC_A", "a")

	shared := &innerConfig{Value: "env.TEST_STATIC_A"}
	cfg := walkConfig{
		Plain:     "env.TEST_STATIC_A",
		Named:     "{env.TEST_STATIC_A}",
		List:      []string{"secret.pw", "x"},
		Headers:   map[string]string{"Authorization": "Bearer {secret.pw}"},
		Inner:     innerConfig{Value: "env.TEST_STATIC_A"},
		Pointer:   shared,
		Child:     map[string]interface{}{"url": "env.TEST_STATIC_A"},
		Any:       "env.TEST_STATIC_A",
		Template:  Template{raw: "{env.TEST_STATIC_A}/{z}"},
		Number:    3,
		Params:    map[string]interface{}{"key": "secret.pw", "nested": []interface{}{"env.TEST_STATIC_A"}},
		unexposed: "env.TEST_STATIC_A",
	}

	require.NoError(t, secretResolver().ResolveInto(&cfg))

	assert.Equal(t, "a", cfg.Plain)
	assert.Equal(t, namedString("a"), cfg.Named)
	assert.Equal(t, []string{"hunter2", "x"}, cfg.List)
	assert.Equal(t, "Bearer hunter2", cfg.Headers["Authorization"])
	assert.Equal(t, "a", cfg.Inner.Value)
	assert.Equal(t, "a", cfg.Pointer.Value)
	assert.Nil(t, cfg.NilPtr)
	assert.Equal(t, 3, cfg.Number)
	assert.Equal(t, "hunter2", cfg.Params["key"])
	assert.Equal(t, []interface{}{"a"}, cfg.Params["nested"])
	assert.Equal(t, "env.TEST_STATIC_A", cfg.unexposed)

	// Child entity configs are resolved when they're decoded
	assert.Equal(t, "env.TEST_STATIC_A", cfg.Child["url"])
	assert.Equal(t, "env.TEST_STATIC_A", cfg.Any)

	assert.True(t, cfg.Template.segs[0].resolved)
	assert.Equal(t, "a", cfg.Template.segs[0].value)

	// The caller's pointed-to values are copied rather than mutated
	assert.Equal(t, "env.TEST_STATIC_A", shared.Value)
}

func Test_ResolveInto_InterfaceHoldingStruct(t *testing.T) {
	t.Setenv("TEST_STATIC_A", "a")

	var cfg any = innerConfig{Value: "env.TEST_STATIC_A"}

	require.NoError(t, NewResolver(context.Background(), nil).ResolveInto(&cfg))
	assert.Equal(t, innerConfig{Value: "a"}, cfg)

	var empty any
	require.NoError(t, NewResolver(context.Background(), nil).ResolveInto(&empty))
	assert.Nil(t, empty)
}

func Test_ResolveInto_Errors(t *testing.T) {
	r := NewResolver(context.Background(), nil)

	require.Error(t, r.ResolveInto(innerConfig{}))
	require.Error(t, r.ResolveInto((*innerConfig)(nil)))

	for name, cfg := range map[string]any{
		"plain":    &walkConfig{Plain: "secret.x"},
		"list":     &walkConfig{List: []string{"secret.x"}},
		"map":      &walkConfig{Headers: map[string]string{"a": "secret.x"}},
		"pointer":  &walkConfig{Pointer: &innerConfig{Value: "secret.x"}},
		"remain":   &walkConfig{Params: map[string]interface{}{"a": "secret.x"}},
		"template": &walkConfig{Template: Template{raw: "{secret.x}"}},
		"whole":    &walkConfig{Template: Template{raw: "secret.x"}},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, r.ResolveInto(cfg))
		})
	}
}

func Test_ResolveRaw_Nil(t *testing.T) {
	out, err := NewResolver(context.Background(), nil).ResolveRaw(nil)

	require.NoError(t, err)
	assert.Nil(t, out)
}

func Test_ResolveRaw_Error(t *testing.T) {
	_, err := NewResolver(context.Background(), nil).ResolveRaw(map[string]interface{}{"a": []interface{}{"secret.x"}})

	require.Error(t, err)
}
