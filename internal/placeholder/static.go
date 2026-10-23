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
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"

	"github.com/Michad/tilegroxy/pkg/entities/secret"
)

var errNoSecretStore = errors.New("no secret store is available")

// Fills in env and secret placeholders. A nil Resolver, or one without a Secreter, still resolves env
type Resolver struct {
	ctx      context.Context
	secreter secret.Secreter
}

func NewResolver(ctx context.Context, secreter secret.Secreter) *Resolver {
	return &Resolver{ctx: ctx, secreter: secreter}
}

func (r *Resolver) context() context.Context {
	if r == nil || r.ctx == nil {
		return context.Background()
	}

	return r.ctx
}

func (r *Resolver) lookup(ref Ref) (string, error) {
	switch ref.Source {
	case SourceEnv:
		if v, ok := os.LookupEnv(ref.Name); ok || !ref.HasDefault {
			return v, nil
		}

		return ref.Default, nil
	case SourceSecret:
		v, err := r.lookupSecret(ref.Name)
		if err != nil && ref.HasDefault {
			slog.WarnContext(r.context(), fmt.Sprintf("secret %v could not be resolved, using its default: %v", ref.Name, err))
			return ref.Default, nil
		}

		return v, err
	default:
		return "", fmt.Errorf("%v is not resolved at startup", ref.Source)
	}
}

func (r *Resolver) lookupSecret(key string) (string, error) {
	if r == nil || r.secreter == nil {
		return "", fmt.Errorf("secret %v: %w", key, errNoSecretStore)
	}

	v, _, err := r.secreter.Lookup(r.context(), key)

	return v, err
}

// Whole value forms take precedence. Substituted text is never scanned again
func (r *Resolver) ResolveString(str string) (string, error) {
	if after, found := strings.CutPrefix(str, SourceEnv+"."); found {
		return r.lookup(Ref{Source: SourceEnv, Name: after})
	}

	if after, found := strings.CutPrefix(str, SourceSecret+"."); found {
		return r.lookup(Ref{Source: SourceSecret, Name: after})
	}

	if !strings.Contains(str, "{") {
		return str, nil
	}

	var out strings.Builder

	for _, seg := range parse(str) {
		switch {
		case seg.ref == nil:
			out.WriteString(seg.literal)
		case isStatic(seg.ref.Source):
			v, err := r.lookup(*seg.ref)
			if err != nil {
				return "", err
			}
			out.WriteString(v)
		default:
			out.WriteString(seg.ref.String())
		}
	}

	return out.String(), nil
}

var templateType = reflect.TypeFor[Template]()

// Resolves every string reachable from ptr. Values held as interfaces are child entity configs, decoded and resolved separately
func (r *Resolver) ResolveInto(ptr any) error {
	target := reflect.ValueOf(ptr)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return errors.New("resolve target must be a non-nil pointer")
	}

	target = target.Elem()
	current := target
	if current.Kind() == reflect.Interface {
		if current.IsNil() {
			return nil
		}
		current = current.Elem()
	}

	resolved, err := r.resolveValue(current, false)
	if err != nil {
		return err
	}

	target.Set(resolved)

	return nil
}

// Opaque parameter bags have no child entities, so their interface values are resolved too
func (r *Resolver) ResolveRaw(raw map[string]any) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}

	resolved, err := r.resolveValue(reflect.ValueOf(raw), true)
	if err != nil {
		return nil, err
	}

	return resolved.Interface().(map[string]any), nil
}

// Copies rather than mutating so a config shared with the caller keeps its placeholders
func (r *Resolver) resolveValue(v reflect.Value, intoInterfaces bool) (reflect.Value, error) {
	if v.Type() == templateType {
		t, err := v.Interface().(Template).resolveStatic(r)
		if err != nil {
			return v, err
		}

		return reflect.ValueOf(t), nil
	}

	switch v.Kind() { //nolint:exhaustive // the default arm handles all remaining kinds
	case reflect.String:
		s, err := r.ResolveString(v.String())
		if err != nil {
			return v, err
		}

		out := reflect.New(v.Type()).Elem()
		out.SetString(s)

		return out, nil
	case reflect.Pointer:
		if v.IsNil() {
			return v, nil
		}

		inner, err := r.resolveValue(v.Elem(), intoInterfaces)
		if err != nil {
			return v, err
		}

		out := reflect.New(v.Type().Elem())
		out.Elem().Set(inner)

		return out, nil
	case reflect.Interface:
		if !intoInterfaces || v.IsNil() {
			return v, nil
		}

		inner, err := r.resolveValue(v.Elem(), intoInterfaces)
		if err != nil {
			return v, err
		}

		out := reflect.New(v.Type()).Elem()
		out.Set(inner)

		return out, nil
	case reflect.Struct:
		return r.resolveStruct(v, intoInterfaces)
	case reflect.Slice:
		if v.IsNil() {
			return v, nil
		}

		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			elem, err := r.resolveValue(v.Index(i), intoInterfaces)
			if err != nil {
				return v, err
			}
			out.Index(i).Set(elem)
		}

		return out, nil
	case reflect.Map:
		if v.IsNil() {
			return v, nil
		}

		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			elem, err := r.resolveValue(iter.Value(), intoInterfaces)
			if err != nil {
				return v, err
			}
			out.SetMapIndex(iter.Key(), elem)
		}

		return out, nil
	default:
		return v, nil
	}
}

func (r *Resolver) resolveStruct(v reflect.Value, intoInterfaces bool) (reflect.Value, error) {
	out := reflect.New(v.Type()).Elem()
	out.Set(v)

	for i := range v.NumField() {
		field := v.Type().Field(i)
		if !field.IsExported() {
			continue
		}

		// A mapstructure remain bag holds the entity's own leftover parameters, never a child entity
		fieldIntoInterfaces := intoInterfaces || strings.Contains(field.Tag.Get("mapstructure"), ",remain")

		resolved, err := r.resolveValue(v.Field(i), fieldIntoInterfaces)
		if err != nil {
			return v, err
		}

		out.Field(i).Set(resolved)
	}

	return out, nil
}
