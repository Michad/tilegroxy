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

//go:build !no_aws

package secrets

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/secret"

	"github.com/maypok86/otter"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

const cacheSize = 10_000

type AWSSecretsManagerConfig struct {
	TTL int32 // How long to cache secrets in seconds. Cache disabled if less than 0. Defaults to 1 hour

	Access  string
	Secret  string
	Region  string
	Profile string

	Separator string

	Endpoint string // For non-AWS (e.g. localstack)
}

type AWSSecretsManager struct {
	AWSSecretsManagerConfig
	client *secretsmanager.Client
	cache  *otter.Cache[string, string]
}

func init() {
	secret.RegisterSecreter(AWSSecretsManagerSecreter{})
}

type AWSSecretsManagerSecreter struct {
}

func (s AWSSecretsManagerSecreter) InitializeConfig() any {
	return AWSSecretsManagerConfig{}
}

func (s AWSSecretsManagerSecreter) Name() string {
	return "awssecretsmanager"
}

func (s AWSSecretsManagerSecreter) Initialize(cfgAny any, _ config.ErrorMessages) (secret.Secreter, error) {
	cfg := cfgAny.(AWSSecretsManagerConfig)
	if cfg.Separator == "" {
		cfg.Separator = ":"
	}
	if cfg.TTL == 0 {
		cfg.TTL = 60 * 60
	}

	awsConfig, err := awsconfig.LoadDefaultConfig(pkg.BackgroundContext(), func(lo *awsconfig.LoadOptions) error {
		if cfg.Profile != "" {
			lo.SharedConfigProfile = cfg.Profile
		}

		if cfg.Region != "" {
			lo.Region = cfg.Region
		}

		if cfg.Access != "" {
			lo.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(cfg.Access, cfg.Secret, ""))
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	svc := secretsmanager.NewFromConfig(awsConfig, func(o *secretsmanager.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = &cfg.Endpoint
		}
	})

	if cfg.TTL > 0 {
		cache, err := otter.MustBuilder[string, string](cacheSize).WithTTL(time.Duration(cfg.TTL) * time.Second).Build()
		if err != nil {
			return nil, err
		}

		return &AWSSecretsManager{cfg, svc, &cache}, nil
	}

	return &AWSSecretsManager{cfg, svc, nil}, nil
}

func (s AWSSecretsManager) Lookup(key string) (string, error) {
	ctx := pkg.BackgroundContext()
	keySplit := strings.Split(key, s.Separator)

	secretName := keySplit[0]
	var secretString string
	isCached := false

	if s.cache != nil {
		secretString, isCached = s.cache.Get(secretName)
	}

	if !isCached {
		input := &secretsmanager.GetSecretValueInput{
			SecretId: aws.String(secretName),
		}

		result, err := s.client.GetSecretValue(ctx, input)
		if err != nil {
			return "", err
		}

		secretString = *result.SecretString

		if s.cache != nil {
			s.cache.Set(secretName, secretString)
		}
	}

	if len(keySplit) > 1 {
		result := make(map[string]interface{})
		if err := json.Unmarshal([]byte(secretString), &result); err == nil {
			secretString, _ = result[keySplit[1]].(string)
		}
	}

	return secretString, nil
}

// Just for testing purposes
func (s AWSSecretsManager) makeSecret(key, val string) error {
	_, err := s.client.CreateSecret(pkg.BackgroundContext(), &secretsmanager.CreateSecretInput{
		Name:         &key,
		SecretString: &val,
	})
	return err
}
