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

//go:build !no_gcp

package secrets

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/secret"

	"github.com/maypok86/otter"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/option"
)

type GCPSecretManagerConfig struct {
	TTL int32 // How long to cache secrets in seconds. Cache disabled if less than 0. Defaults to 1 hour

	Project string // The GCP project id containing the secrets. Required

	// The version of a secret to use when a key doesn't specify one explicitly. Defaults to "latest"
	Version string

	CredentialsFile string // Path to a service account JSON key file. Defaults to Application Default Credentials

	Separator string

	Endpoint string // Overrides the Secret Manager API endpoint (e.g. for a local emulator)
}

type GCPSecretManager struct {
	GCPSecretManagerConfig
	client *secretmanager.Client
	cache  *otter.Cache[string, string]
}

func init() {
	secret.RegisterSecreter(GCPSecretManagerSecreter{})
}

type GCPSecretManagerSecreter struct {
}

func (s GCPSecretManagerSecreter) InitializeConfig() any {
	return GCPSecretManagerConfig{}
}

func (s GCPSecretManagerSecreter) Name() string {
	return "gcpsecretmanager"
}

func (s GCPSecretManagerSecreter) Initialize(cfgAny any, deps secret.SecreterDeps) (secret.Secreter, error) {
	cfg := cfgAny.(GCPSecretManagerConfig)

	if cfg.Project == "" {
		return nil, fmt.Errorf(deps.ErrorMessages.ParamRequired, "secret.gcpsecretmanager.project")
	}

	if cfg.Separator == "" {
		cfg.Separator = ":"
	}
	if cfg.Version == "" {
		cfg.Version = "latest"
	}
	if cfg.TTL == 0 {
		cfg.TTL = 60 * 60
	}

	var opts []option.ClientOption

	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	}

	if cfg.Endpoint != "" {
		opts = append(opts, option.WithEndpoint(cfg.Endpoint))

		// A custom endpoint implies a non-production target (e.g. an emulator in tests), which
		// won't accept normal Google authentication. Real Secret Manager traffic never sets this
		if cfg.CredentialsFile == "" {
			opts = append(opts, option.WithoutAuthentication())
		}
	}

	// Uses the REST transport rather than gRPC. Secret Manager fully supports both; REST keeps
	// this in line with how the rest of tilegroxy talks to external HTTP services and makes the
	// endpoint override usable against a plain HTTP fake in tests
	client, err := secretmanager.NewRESTClient(pkg.BackgroundContext(), opts...)
	if err != nil {
		return nil, err
	}

	if cfg.TTL > 0 {
		cache, err := otter.MustBuilder[string, string](cacheSize).WithTTL(time.Duration(cfg.TTL) * time.Second).Build()
		if err != nil {
			return nil, err
		}

		return &GCPSecretManager{cfg, client, &cache}, nil
	}

	return &GCPSecretManager{cfg, client, nil}, nil
}

func (s GCPSecretManager) Lookup(key string) (string, error) {
	ctx := pkg.BackgroundContext()
	keySplit := strings.Split(key, s.Separator)

	secretName := keySplit[0]
	var secretString string
	isCached := false

	if s.cache != nil {
		secretString, isCached = s.cache.Get(secretName)
	}

	if !isCached {
		version := s.Version
		if len(keySplit) > 2 {
			version = keySplit[2]
		}

		req := &secretmanagerpb.AccessSecretVersionRequest{
			Name: fmt.Sprintf("projects/%s/secrets/%s/versions/%s", s.Project, secretName, version),
		}

		result, err := s.client.AccessSecretVersion(ctx, req)
		if err != nil {
			return "", err
		}

		secretString = string(result.GetPayload().GetData())

		if s.cache != nil {
			s.cache.Set(secretName, secretString)
		}
	}

	if len(keySplit) > 1 && keySplit[1] != "" {
		result := make(map[string]interface{})
		if err := json.Unmarshal([]byte(secretString), &result); err == nil {
			secretString, _ = result[keySplit[1]].(string)
		}
	}

	return secretString, nil
}
