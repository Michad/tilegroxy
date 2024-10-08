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

//todo: go:build !no-aws

package caches

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/config"
	"github.com/Michad/tilegroxy/pkg/entities/cache"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Config struct {
	Bucket       string
	Access       string
	Secret       string
	Region       string
	Path         string
	Profile      string
	StorageClass string
	Endpoint     string // For directory buckets or non-s3
	UsePathStyle bool   // For testing purposes and maybe real non-S3 usage
}

type S3 struct {
	S3Config
	client     *s3.Client
	downloader *manager.Downloader
	uploader   *manager.Uploader
}

func init() {
	cache.RegisterCache(S3Registration{})
}

type S3Registration struct {
}

func (s S3Registration) InitializeConfig() any {
	return S3Config{}
}

func (s S3Registration) Name() string {
	return "s3"
}

func (s S3Registration) Initialize(configAny any, errorMessages config.ErrorMessages) (cache.Cache, error) {
	config := configAny.(S3Config)
	if (config.Access != "" && config.Secret == "") || (config.Access == "" && config.Secret != "") {
		return nil, fmt.Errorf(errorMessages.ParamsBothOrNeither, "cache.s3.access", "cache.s3.secret")
	}

	// Ensure path doesn't start with / but does end with one
	if strings.Index(config.Path, "/") == 0 && len(config.Path) > 0 {
		config.Path = config.Path[1:]
	}
	if strings.LastIndex(config.Path, "/") != len(config.Path)-1 {
		config.Path += "/"
	}

	if config.Bucket == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "cache.s3.bucket", config.Bucket)
	}

	awsConfig, err := awsconfig.LoadDefaultConfig(pkg.BackgroundContext(), func(lo *awsconfig.LoadOptions) error {

		if config.Profile != "" {
			lo.SharedConfigProfile = config.Profile
		}

		if config.Region != "" {
			lo.Region = config.Region
		}

		if config.Access != "" {
			lo.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(config.Access, config.Secret, ""))
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if config.StorageClass != "" {
		validValues := types.StorageClass.Values("")

		if !slices.Contains(validValues, types.StorageClass(config.StorageClass)) {
			return nil, fmt.Errorf(errorMessages.EnumError, "cache.s3.storageclass", config.StorageClass, validValues)
		}
	}

	client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		if config.Endpoint != "" {
			o.BaseEndpoint = &config.Endpoint
		}
		o.UsePathStyle = config.UsePathStyle
	})

	downloader := manager.NewDownloader(client)
	uploader := manager.NewUploader(client)

	return &S3{config, client, downloader, uploader}, nil
}

func calcKey(config *S3, t *pkg.TileRequest) string {
	return config.Path + t.LayerName + "/" + strconv.Itoa(t.Z) + "/" + strconv.Itoa(t.X) + "/" + strconv.Itoa(t.Y)
}

// Just for testing purposes
func (c S3) makeBucket() error {
	_, err := c.client.CreateBucket(pkg.BackgroundContext(), &s3.CreateBucketInput{Bucket: &c.Bucket})
	return err
}

func (c S3) Lookup(ctx context.Context, t pkg.TileRequest) (*pkg.Image, error) {
	writer := manager.NewWriteAtBuffer([]byte{})

	_, err := c.downloader.Download(
		ctx,
		writer,
		&s3.GetObjectInput{
			Bucket: aws.String(c.Bucket),
			Key:    aws.String(calcKey(&c, &t)),
		})

	if err != nil {
		var requestFailure *types.NoSuchKey
		if errors.As(err, &requestFailure) {
			// Simple cache miss
			return nil, nil
		}

		return nil, err
	}

	b := writer.Bytes()

	return pkg.DecodeImage(b)
}

func (c S3) Save(ctx context.Context, t pkg.TileRequest, img *pkg.Image) error {
	b, err := img.Encode()

	if err != nil {
		return err
	}

	uploadConfig := &s3.PutObjectInput{
		Bucket: &c.Bucket,
		Key:    aws.String(calcKey(&c, &t)),
		Body:   bytes.NewReader(b),
	}

	if c.StorageClass != "" {
		uploadConfig.StorageClass = types.StorageClass(c.StorageClass)
	}

	_, err = c.uploader.Upload(ctx, uploadConfig)
	return err
}
