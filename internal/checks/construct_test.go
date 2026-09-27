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

package checks

import (
	"context"
	"testing"

	"github.com/Michad/tilegroxy/internal/layers"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubHealthCheckConfig struct {
	Delay uint
}

func (s stubHealthCheckConfig) GetDelay() uint { return s.Delay }

type stubHealthCheck struct {
	delay uint
	fail  bool
}

func (s stubHealthCheck) Check(_ context.Context) error {
	if s.fail {
		return assert.AnError
	}
	return nil
}

func (s stubHealthCheck) GetDelay() uint { return s.delay }

type stubHealthCheckRegistration struct{}

func (stubHealthCheckRegistration) Name() string { return "stub-check" }
func (stubHealthCheckRegistration) InitializeConfig() health.HealthCheckConfig {
	return stubHealthCheckConfig{}
}
func (stubHealthCheckRegistration) Initialize(cfg health.HealthCheckConfig, _ health.HealthCheckDeps) (health.HealthCheck, error) {
	sc := cfg.(stubHealthCheckConfig)
	return stubHealthCheck{delay: sc.Delay}, nil
}

func init() {
	health.RegisterHealthCheck(stubHealthCheckRegistration{})
}

func testLayerGroup(t *testing.T) *layers.LayerGroup {
	t.Helper()
	lg, err := layers.ConstructLayerGroup(context.Background(), config.Config{}, nil, nil, nil)
	require.NoError(t, err)
	return lg
}

func Test_ConstructHealthCheck_UnknownNameErrors(t *testing.T) {
	cfg := config.DefaultConfig()
	_, err := ConstructHealthCheck(map[string]interface{}{"name": "not-a-real-check"}, testLayerGroup(t), nil, &cfg)
	require.Error(t, err)
}

func Test_ConstructHealthCheck_ConstructsRegisteredCheck(t *testing.T) {
	cfg := config.DefaultConfig()
	hc, err := ConstructHealthCheck(map[string]interface{}{"name": "stub-check", "delay": 5}, testLayerGroup(t), nil, &cfg)
	require.NoError(t, err)
	require.NotNil(t, hc)

	assert.Equal(t, uint(5), hc.GetDelay())
	assert.NoError(t, hc.Check(context.Background()))
}
