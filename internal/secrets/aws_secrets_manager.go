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
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/entities/secret"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type AWSSecretsManagerConfig struct {
	Access  string
	Secret  string
	Region  string
	Profile string

	Separator string

	Endpoint string // For non-AWS (e.g. localstack)
}

// awsSecretValue is one fetched secret, kept so sibling JSON keys reuse a single API call
type awsSecretValue struct {
	value   string
	version string
}

type AWSSecretsManager struct {
	AWSSecretsManagerConfig
	client *secretsmanager.Client

	mu        sync.Mutex
	nameCache map[string]awsSecretValue
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

func (s AWSSecretsManagerSecreter) Initialize(cfgAny any, _ secret.SecreterDeps) (secret.Secreter, error) {
	cfg := cfgAny.(AWSSecretsManagerConfig)
	if cfg.Separator == "" {
		cfg.Separator = ":"
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

	return &AWSSecretsManager{AWSSecretsManagerConfig: cfg, client: svc, nameCache: make(map[string]awsSecretValue)}, nil
}

func (s *AWSSecretsManager) Lookup(ctx context.Context, key string) (string, string, error) {
	keySplit := strings.Split(key, s.Separator)
	secretName := keySplit[0]

	entry, err := s.fetch(ctx, secretName)
	if err != nil {
		return "", "", err
	}

	secretString := entry.value

	if len(keySplit) > 1 {
		result := make(map[string]interface{})
		if err := json.Unmarshal([]byte(secretString), &result); err == nil {
			secretString, _ = result[keySplit[1]].(string)
		}
	}

	return secretString, entry.version, nil
}

func (s *AWSSecretsManager) fetch(ctx context.Context, secretName string) (awsSecretValue, error) {
	s.mu.Lock()
	entry, ok := s.nameCache[secretName]
	s.mu.Unlock()

	if ok {
		return entry, nil
	}

	result, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	})
	if err != nil {
		return awsSecretValue{}, err
	}

	if result.SecretString == nil {
		return awsSecretValue{}, fmt.Errorf("aws secret %s has no string value, binary secrets are not supported", secretName)
	}

	entry = awsSecretValue{value: *result.SecretString}
	if result.VersionId != nil {
		entry.version = *result.VersionId
	}

	s.mu.Lock()
	s.nameCache[secretName] = entry
	s.mu.Unlock()

	return entry, nil
}

const awsCurrentStage = "AWSCURRENT"

func (s *AWSSecretsManager) Check(ctx context.Context, keys []string) ([]string, error) {
	// Sibling JSON keys share one secret, so describe each distinct name once
	byName := make(map[string]string, len(keys))

	for _, key := range keys {
		name := strings.Split(key, s.Separator)[0]
		if _, done := byName[name]; done {
			continue
		}

		version, err := s.describeCurrentVersion(ctx, name)
		if err != nil {
			return nil, err
		}

		byName[name] = version
	}

	versions := make([]string, len(keys))
	for i, key := range keys {
		versions[i] = byName[strings.Split(key, s.Separator)[0]]
	}

	return versions, nil
}

func (s *AWSSecretsManager) describeCurrentVersion(ctx context.Context, secretName string) (string, error) {
	out, err := s.client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(secretName),
	})
	if err != nil {
		return "", err
	}

	for versionID, stages := range out.VersionIdsToStages {
		for _, stage := range stages {
			if stage == awsCurrentStage {
				return versionID, nil
			}
		}
	}

	// No current version means nothing to compare against, which the caller treats as unwatchable
	return "", nil
}

func (s AWSSecretsManagerSecreter) CheckBatchSize() int {
	return 1
}

// Just for testing purposes
func (s *AWSSecretsManager) makeSecret(key, val string) error {
	_, err := s.client.CreateSecret(pkg.BackgroundContext(), &secretsmanager.CreateSecretInput{
		Name:         &key,
		SecretString: &val,
	})
	return err
}
