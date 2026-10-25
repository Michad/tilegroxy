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

package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/Michad/tilegroxy/internal/configload"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testLayers = `
layers:
  - id: basemap
    provider:
      name: static
      color: FFF
  - id: satellite
    ratelimit: premium
    provider:
      name: static
      color: FFF
  - id: internal_debug
    skipratelimit: true
    provider:
      name: static
      color: FFF
`

const testAuth = `
authentication:
  name: static key
  key: abc
`

func loadConfig(t *testing.T, raw string) config.Config {
	t.Helper()

	cfg, err := configload.LoadConfig(raw)
	require.NoError(t, err)

	return cfg
}

func construct(t *testing.T, raw string) (*Limiter, error) {
	t.Helper()

	cfg := loadConfig(t, raw)

	return ConstructLimiter(context.Background(), cfg, nil, ratelimit.RateLimiterDeps{ErrorMessages: cfg.Error.Messages})
}

func Test_Construct_DocsExample(t *testing.T) {
	l, err := construct(t, `
ratelimit:
  name: memory
  headers: standard
  onError: allow
  policies:
    - id: standard
      rules:
        - segment:
            - user
          limit: 50
          period: 1
    - id: premium
      extend:
        - standard
      rules:
        - segment:
            - tenant
          limit: 100000
          calendar: month
          overrides:
            - tenants:
                - acme
              limit: 5000000
defaultratelimit: standard
`+testAuth+testLayers)
	require.NoError(t, err)

	assert.True(t, l.Enabled())
	assert.Equal(t, HeadersStandard, l.headers)
	assert.Equal(t, OnErrorAllow, l.onError)
	assert.Equal(t, defaultTimeout, l.timeout)
	assert.Equal(t, defaultIPv6Prefix, l.ipv6Prefix)

	assert.Equal(t, "standard", l.layerPolicies["basemap"].id)
	assert.Equal(t, "premium", l.layerPolicies["satellite"].id)
	assert.NotContains(t, l.layerPolicies, "internal_debug")

	premium := l.byID["premium"]
	require.Len(t, premium.all, 2)
	assert.Equal(t, "standard.user.1s", premium.all[0].name)
	assert.Equal(t, "premium.tenant.month", premium.all[1].name)
	assert.Same(t, l.byID["standard"].rules[0], premium.all[0])
	assert.Equal(t, int64(5000000), premium.all[1].overrides[0].limit)
}

func Test_Construct_JSONAndOptions(t *testing.T) {
	l, err := construct(t, `{
  "ratelimit": {
    "name": "memory",
    "maxsize": 10,
    "headers": "MIN",
    "onError": "deny",
    "timeout": 250,
    "ipv6Prefix": 48,
    "policies": [{"id": "a", "rules": [{"segment": [], "limit": 5, "period": 60, "overrides": [{"ips": ["10.0.0.0/8"], "limit": -1}]}]}]
  },
  "layers": [{"id": "l", "provider": {"name": "static", "color": "FFF"}}]
}`)
	require.NoError(t, err)

	assert.Equal(t, HeadersMin, l.headers)
	assert.Equal(t, OnErrorDeny, l.onError)
	assert.Equal(t, 250*time.Millisecond, l.timeout)
	assert.Equal(t, 48, l.ipv6Prefix)
	assert.Equal(t, "a.all.60s", l.layerPolicies["l"].all[0].name)
	assert.Equal(t, int64(-1), l.layerPolicies["l"].all[0].overrides[0].limit)
}

func Test_Construct_DisabledByDefault(t *testing.T) {
	l, err := construct(t, `
layers:
  - id: l
    provider:
      name: static
      color: FFF
`)
	require.NoError(t, err)

	assert.False(t, l.Enabled())
	assert.Equal(t, Result{Outcome: Allowed}, l.Check(context.Background(), "l"))
}

func Test_Construct_NoneStillValidates(t *testing.T) {
	l, err := construct(t, `
ratelimit:
  name: none
  policies:
    - id: a
      rules:
        - segment: [ip]
          limit: 5
          period: 1
`+testLayers)
	require.Error(t, err, "satellite names a policy that doesn't exist")
	assert.Nil(t, l)

	_, err = construct(t, `
ratelimit:
  name: none
  policies:
    - id: a
      rules:
        - segment: [ip]
          limit: 0
          period: 1
`)
	require.Error(t, err)
}

func Test_Construct_FirstPolicyIsDefault(t *testing.T) {
	l, err := construct(t, `
ratelimit:
  name: memory
  policies:
    - id: first
    - id: premium
`+testLayers)
	require.NoError(t, err)

	assert.Equal(t, "first", l.layerPolicies["basemap"].id)
	assert.Empty(t, l.layerPolicies["basemap"].all)
}

func assertConstructErrors(t *testing.T, cases map[string]string) {
	t.Helper()

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := construct(t, raw)
			require.Error(t, err)
		})
	}
}

func rulePolicy(rule string) string {
	return `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - ` + rule + "\n" + testAuth
}

func Test_Construct_OptionsErrors(t *testing.T) {
	assertConstructErrors(t, map[string]string{
		"unknown store": `
ratelimit:
  name: nope
`,
		"no policies": `
ratelimit:
  name: memory
`,
		"unknown common key": `
ratelimit:
  name: memory
  polices: []
`,
		"unknown store key": `
ratelimit:
  name: memory
  maxsizes: 4
  policies:
    - id: a
`,
		"bad headers": `
ratelimit:
  name: memory
  headers: all
  policies:
    - id: a
`,
		"bad onerror": `
ratelimit:
  name: memory
  onerror: maybe
  policies:
    - id: a
`,
		"bad ipv6prefix": `
ratelimit:
  name: memory
  ipv6prefix: 129
  policies:
    - id: a
`,
	})
}

func Test_Construct_PoliciesErrors(t *testing.T) {
	assertConstructErrors(t, map[string]string{
		"missing id": `
ratelimit:
  name: memory
  policies:
    - rules: []
`,
		"bad id": `
ratelimit:
  name: memory
  policies:
    - id: a.b
`,
		"duplicate id": `
ratelimit:
  name: memory
  policies:
    - id: a
    - id: a
`,
		"duplicate rule": `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [ip, layer], limit: 1, period: 1}
        - {segment: [layer, ip], limit: 5, period: 1}
`,
		"unknown extend": `
ratelimit:
  name: memory
  policies:
    - id: a
      extend: [b]
`,
		"extend loop": `
ratelimit:
  name: memory
  policies:
    - id: a
      extend: [b]
    - id: b
      extend: [c]
    - id: c
      extend: [a]
`,
		"user without auth": `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [user], limit: 1, period: 1}
`,
		"unknown default": `
ratelimit:
  name: memory
  policies:
    - id: a
defaultratelimit: b
`,
		"unknown layer policy": `
ratelimit:
  name: memory
  policies:
    - id: a
` + testLayers,
		"skip and policy": `
ratelimit:
  name: memory
  policies:
    - id: a
layers:
  - id: l
    ratelimit: a
    skipratelimit: true
    provider:
      name: static
      color: FFF
`,
	})
}

func Test_Construct_RulesErrors(t *testing.T) {
	assertConstructErrors(t, map[string]string{
		"unknown rule key": rulePolicy(`segment: [ip]
          limit: 5
          period: 1
          limt: 3`),
		"zero limit":         rulePolicy("{segment: [ip], limit: 0, period: 1}"),
		"no period":          rulePolicy("{segment: [ip], limit: 1}"),
		"period and cal":     rulePolicy("{segment: [ip], limit: 1, period: 1, calendar: day}"),
		"bad calendar":       rulePolicy("{segment: [ip], limit: 1, calendar: fortnight}"),
		"missing segment":    rulePolicy("{limit: 1, period: 1}"),
		"bad segment":        rulePolicy("{segment: [country], limit: 1, period: 1}"),
		"repeated segment":   rulePolicy("{segment: [ip, ip], limit: 1, period: 1}"),
		"empty override":     rulePolicy("{segment: [ip], limit: 1, period: 1, overrides: [{limit: 3}]}"),
		"no override limit":  rulePolicy("{segment: [ip], limit: 1, period: 1, overrides: [{users: [a]}]}"),
		"low override limit": rulePolicy("{segment: [ip], limit: 1, period: 1, overrides: [{users: [a], limit: -2}]}"),
		"bad override ip":    rulePolicy("{segment: [ip], limit: 1, period: 1, overrides: [{ips: [nope], limit: 2}]}"),
	})
}

func Test_Construct_StoresErrors(t *testing.T) {
	assertConstructErrors(t, map[string]string{
		"maxsize zero": `
ratelimit:
  name: memory
  maxsize: 0
  policies:
    - id: a
`,
		"redis without datastore": `
ratelimit:
  name: redis
  policies:
    - id: a
`,
		"redis unknown datastore": `
ratelimit:
  name: redis
  datastore: nope
  policies:
    - id: a
`,
		"postgresql without datastore": `
ratelimit:
  name: postgresql
  table: t
  policies:
    - id: a
`,
		"postgresql without table": `
ratelimit:
  name: postgresql
  datastore: pg
  policies:
    - id: a
`,
		"postgresql bad table": `
ratelimit:
  name: postgresql
  datastore: pg
  table: "t; drop table x"
  policies:
    - id: a
`,
		"postgresql unknown datastore": `
ratelimit:
  name: postgresql
  datastore: pg
  table: s.t
  policies:
    - id: a
`,
	})
}

func Test_Construct_SegmentsAreCanonical(t *testing.T) {
	l, err := construct(t, `
ratelimit:
  name: memory
  policies:
    - id: a
      rules:
        - {segment: [LAYER, ip, tenant, user], limit: 1, calendar: Day}
`+testAuth)
	require.NoError(t, err)

	assert.Equal(t, "a.user+tenant+ip+layer.day", l.byID["a"].rules[0].name)
}

func Test_Construct_ExtendReachedTwiceCheckedOnce(t *testing.T) {
	l, err := construct(t, `
ratelimit:
  name: memory
  policies:
    - id: base
      rules:
        - {segment: [ip], limit: 1, period: 1}
    - id: mid1
      extend: [base]
    - id: mid2
      extend: [base]
      rules:
        - {segment: [ip], limit: 1, period: 2}
    - id: top
      extend: [mid1, mid2]
`)
	require.NoError(t, err)

	names := make([]string, 0, len(l.byID["top"].all))
	for _, r := range l.byID["top"].all {
		names = append(names, r.name)
	}

	assert.Equal(t, []string{"base.ip.1s", "mid2.ip.2s"}, names)
}

func Test_StoreDatastoreID(t *testing.T) {
	t.Setenv("RL_DS", "redis-9")

	assert.Equal(t, "redis-0", StoreDatastoreID(map[string]interface{}{"name": "redis", "datastore": "redis-0"}))
	assert.Equal(t, "redis-9", StoreDatastoreID(map[string]interface{}{"name": "redis", "Datastore": "env.RL_DS"}))
	assert.Empty(t, StoreDatastoreID(map[string]interface{}{"name": "memory"}))
}

func Test_ParsePrefix(t *testing.T) {
	cases := map[string]string{
		"10.1.2.3":                "10.1.2.3/32",
		"10.1.2.3/8":              "10.0.0.0/8",
		"::ffff:10.1.2.3":         "10.1.2.3/32",
		"::ffff:10.1.2.3/104":     "10.0.0.0/8",
		"2001:db8::1/32":          "2001:db8::/32",
		"fe80::1%eth0":            "fe80::1/128",
		"2001:db8:aaaa:bbbb::1/0": "::/0",
	}

	for raw, want := range cases {
		p, err := ParsePrefix(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, p.String(), raw)
	}

	for _, raw := range []string{"nope", "10.0.0.0/33", ""} {
		_, err := ParsePrefix(raw)
		require.Error(t, err, raw)
	}
}
