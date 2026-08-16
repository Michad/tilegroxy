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
	"errors"
	"sync"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fake is a minimal Analytics used to observe what the registry hands to each module.
type fake struct {
	mutex  sync.Mutex
	events []Event
	closed bool
	err    error
}

func (f *fake) Record(_ context.Context, event Event) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.events = append(f.events, event)

	return f.err
}

func (f *fake) Close(_ context.Context) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.closed = true

	return nil
}

func (f *fake) snapshot() []Event {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	out := make([]Event, len(f.events))
	copy(out, f.events)

	return out
}

type fakeConfig struct {
	CommonConfig `mapstructure:",squash"`
}

// fakeRegistration lets these tests exercise the real construction path without depending on
// a concrete module in internal/, which would be an import cycle.
type fakeRegistration struct {
	instances *[]*fake
}

func (s fakeRegistration) InitializeConfig() any { return fakeConfig{} }
func (s fakeRegistration) Name() string          { return "testfake" }

func (s fakeRegistration) Initialize(_ any, _ AnalyticsDeps) (Analytics, error) {
	f := &fake{}
	*s.instances = append(*s.instances, f)

	return f, nil
}

func Test_ConstructAnalytics_UnknownName(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	_, err := ConstructAnalytics(map[string]interface{}{"name": "nosuchmodule"}, nil, AnalyticsDeps{ErrorMessages: msgs})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nosuchmodule")
}

func Test_ConstructAnalytics_MissingName(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	_, err := ConstructAnalytics(map[string]interface{}{}, nil, AnalyticsDeps{ErrorMessages: msgs})
	require.Error(t, err)
}

func Test_ConstructAnalytics_DefaultsIDToName(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	var instances []*fake
	RegisterAnalytics(fakeRegistration{instances: &instances})

	a, err := ConstructAnalytics(map[string]interface{}{"name": "testfake"}, nil, AnalyticsDeps{ErrorMessages: msgs})
	require.NoError(t, err)
	assert.Equal(t, "testfake", a.ID)

	b, err := ConstructAnalytics(map[string]interface{}{"name": "testfake", "id": "custom"}, nil, AnalyticsDeps{ErrorMessages: msgs})
	require.NoError(t, err)
	assert.Equal(t, "custom", b.ID)
}

func Test_AnalyticsWrapper_Empty(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	var instances []*fake
	RegisterAnalytics(fakeRegistration{instances: &instances})

	a, err := ConstructAnalytics(map[string]interface{}{"name": "testfake"}, nil, AnalyticsDeps{ErrorMessages: msgs})
	require.NoError(t, err)
	assert.False(t, a.Empty())

	// The default configuration selects the noop module, which the handler should skip entirely.
	assert.True(t, (&AnalyticsWrapper{Name: noneName}).Empty())

	// A nil wrapper is what tests and the seed/test commands end up with.
	var nilWrapper *AnalyticsWrapper
	assert.True(t, nilWrapper.Empty())
	assert.NoError(t, nilWrapper.Close(context.Background()))
	assert.NotPanics(t, func() { nilWrapper.RecordEvent(context.Background(), Event{}, FieldSource{}) })
}

func Test_AnalyticsWrapper_RecordEventResolvesFields(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	var instances []*fake
	RegisterAnalytics(fakeRegistration{instances: &instances})

	a, err := ConstructAnalytics(map[string]interface{}{
		"name":        "testfake",
		"fields":      []string{"contenttype"},
		"extrafields": map[string]string{"env": "prod"},
	}, nil, AnalyticsDeps{ErrorMessages: msgs})
	require.NoError(t, err)
	require.Len(t, instances, 1)

	a.RecordEvent(pkg.BackgroundContext(), Event{LayerID: "main", Z: 1, X: 2, Y: 3}, FieldSource{ContentType: "image/png"})

	recorded := instances[0].snapshot()
	require.Len(t, recorded, 1)

	assert.Equal(t, "main", recorded[0].LayerID)
	assert.Equal(t, "image/png", recorded[0].Fields[FieldContentType])
	assert.Equal(t, "prod", recorded[0].Fields["env"])
}

func Test_ConstructAnalytics_RejectsBadFieldConfig(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	var instances []*fake
	RegisterAnalytics(fakeRegistration{instances: &instances})

	_, err := ConstructAnalytics(map[string]interface{}{
		"name": "testfake", "fields": []string{"bogus"},
	}, nil, AnalyticsDeps{ErrorMessages: msgs})

	require.Error(t, err, "field validation should happen during construction, not at request time")
}

func Test_AnalyticsWrapper_Close(t *testing.T) {
	msgs := config.DefaultConfig().Error.Messages

	var instances []*fake
	RegisterAnalytics(fakeRegistration{instances: &instances})

	a, err := ConstructAnalytics(map[string]interface{}{"name": "testfake"}, nil, AnalyticsDeps{ErrorMessages: msgs})
	require.NoError(t, err)
	require.Len(t, instances, 1)

	require.NoError(t, a.Close(context.Background()))
	assert.True(t, instances[0].closed, "Close must reach through the wrapper to the module")
}

func Test_AnalyticsWrapper_AbsorbsErrors(t *testing.T) {
	f := &fake{err: errors.New("destination unreachable")}
	w := AnalyticsWrapper{Name: "testfake", ID: "x", Analytics: f}

	// A failing analytics destination must never produce an error the request path could act on.
	require.NoError(t, w.Record(pkg.BackgroundContext(), Event{LayerID: "main"}))
	assert.Len(t, f.snapshot(), 1)
}

func Test_RegisteredAnalyticsNames(t *testing.T) {
	var instances []*fake
	RegisterAnalytics(fakeRegistration{instances: &instances})

	assert.Contains(t, RegisteredAnalyticsNames(), "testfake")

	_, ok := RegisteredAnalytics("testfake")
	assert.True(t, ok)

	_, ok = RegisteredAnalytics("nope")
	assert.False(t, ok)
}
