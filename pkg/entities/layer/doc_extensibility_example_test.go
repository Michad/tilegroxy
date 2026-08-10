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

package layer

import (
	"context"
	"errors"
	"testing"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/datastore"
	"github.com/stretchr/testify/require"
)

// Mirrors the "Sample" provider example in docs/operation/modules/ROOT/pages/extensibility.adoc
// word for word, with types renamed to avoid collisions here. AsciiDoc code blocks aren't
// compiled, so this is what fails CI when the Provider interface changes and the doc goes stale.
// Keep the two in sync by hand.

type docExampleSampleConfig struct {
	// Insert configuration for your provider here
}

type docExampleSample struct {
	docExampleSampleConfig
	// Add any resources your provider needs to retain through its lifecycle here.
}

type docExampleSampleRegistration struct {
}

func (s docExampleSampleRegistration) InitializeConfig() any {
	return docExampleSampleConfig{}
}

func (s docExampleSampleRegistration) Name() string {
	return "doc-example-sample"
}

func (s docExampleSampleRegistration) Initialize(cfgAny any, _ config.ClientConfig, _ config.ErrorMessages, _ *LayerGroup, _ *datastore.DatastoreRegistry) (Provider, error) {
	cfg := cfgAny.(docExampleSampleConfig)
	return &docExampleSample{cfg}, nil
}

func (t docExampleSample) PreAuth(_ context.Context, providerContext ProviderContext) (ProviderContext, error) {
	return providerContext, nil
}

func (t docExampleSample) GenerateTile(_ context.Context, _ ProviderContext, _ pkg.TileRequest) (*pkg.Image, error) {
	return nil, errors.New("not implemented")
}

func Test_DocExtensibilityExample_RegistersAndConstructs(t *testing.T) {
	RegisterProvider(docExampleSampleRegistration{})

	provider, err := ConstructProvider(map[string]interface{}{"name": "doc-example-sample"}, config.ClientConfig{}, config.ErrorMessages{}, nil, nil)

	require.NoError(t, err)
	require.NotNil(t, provider)

	_, err = provider.GenerateTile(context.Background(), ProviderContext{}, pkg.TileRequest{})
	require.Error(t, err)
}
