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

package caches

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
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
	StorageClass string //STANDARD | REDUCED_REDUNDANCY | STANDARD_IA | ONEZONE_IA | INTELLIGENT_TIERING | GLACIER |  DEEP_ARCHIVE  |  GLACIER_IR
	Endpoint     string //For directory buckets or non-s3
	UsePathStyle bool   //For testing purposes and maybe real non-S3 usage
}

type S3 struct {
	*S3Config
	client     *s3.Client
	downloader *manager.Downloader
	uploader   *manager.Uploader
}

func ConstructS3(config *S3Config, errorMessages *config.ErrorMessages) (*S3, error) {
	if (config.Access != "" && config.Secret == "") || (config.Access == "" && config.Secret != "") {
		return nil, fmt.Errorf(errorMessages.ParamsBothOrNeither, "cache.s3.access", "cache.s3.secret")
	}

	//Ensure path starts and ends with a /
	if strings.Index(config.Path, "/") != 0 {
		config.Path = "/" + config.Path
	}
	if strings.LastIndex(config.Path, "/") != len(config.Path)-1 {
		config.Path = config.Path + "/"
	}

	if config.Bucket == "" {
		return nil, fmt.Errorf(errorMessages.InvalidParam, "cache.s3.bucket", config.Bucket)
	}

	// sessionOptions := session.Options{}

	awsConfig, err := awsconfig.LoadDefaultConfig(internal.BackgroundContext(), func(lo *awsconfig.LoadOptions) error {

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

	// awsConfig := aws.Config{CredentialsChainVerboseErrors: aws.Bool(true)}

	if config.StorageClass != "" {
		validValues := types.StorageClass.Values("")

		if !slices.Contains(validValues, types.StorageClass(config.StorageClass)) {
			return nil, fmt.Errorf(errorMessages.EnumError, "cache.s3.storageclass", config.StorageClass, validValues)
		}

		// if strings.Contains(config.StorageClass, "ONEZONE") {
		// 	//Directory AKA Express One Zone fails if an MD5 header set. The Go SDK requires you find this obscure flag to disable that
		// 	awsConfig.WithS3DisableContentMD5Validation(true)
		// }
	}

	client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		o.BaseEndpoint = &config.Endpoint
		o.UsePathStyle = config.UsePathStyle
		// o.MD
	})

	downloader := manager.NewDownloader(client)
	uploader := manager.NewUploader(client)

	return &S3{config, client, downloader, uploader}, nil
}

func calcKey(config *S3, t *internal.TileRequest) string {
	return config.Path + t.LayerName + "/" + strconv.Itoa(t.Z) + "/" + strconv.Itoa(t.X) + "/" + strconv.Itoa(t.Y)
}

// Just for testing purposes
func (c S3) makeBucket() error {
	_, err := c.client.CreateBucket(internal.BackgroundContext(), &s3.CreateBucketInput{Bucket: &c.Bucket})
	return err
}

func (c S3) Lookup(t internal.TileRequest) (*internal.Image, error) {
	writer := manager.NewWriteAtBuffer([]byte{})

	_, err := c.downloader.Download(
		context.TODO(),
		writer,
		&s3.GetObjectInput{
			Bucket: aws.String(c.Bucket),
			Key:    aws.String(calcKey(&c, &t)),
		})

	if err != nil {
		var requestFailure *types.NoSuchKey
		if errors.As(err, &requestFailure) {
			//Simple cache miss
			return nil, nil
		}

		return nil, err
	}

	img := internal.Image(writer.Bytes())

	return &img, nil
}

func (c S3) Save(t internal.TileRequest, img *internal.Image) error {

	uploadConfig := &s3.PutObjectInput{
		Bucket: &c.Bucket,
		Key:    aws.String(calcKey(&c, &t)),
		Body:   bytes.NewReader(*img),
	}

	if c.StorageClass != "" {
		uploadConfig.StorageClass = types.StorageClass(c.StorageClass)
	}

	_, err := c.uploader.Upload(context.TODO(), uploadConfig)
	return err
}
