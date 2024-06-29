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
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal"
	"github.com/Michad/tilegroxy/internal/config"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type S3Config struct {
	Bucket       string
	Access       string
	Secret       string
	Region       string
	Path         string
	Profile      string
	StorageClass string //STANDARD | REDUCED_REDUNDANCY | STANDARD_IA | ONEZONE_IA | INTELLIGENT_TIERING | GLACIER |  DEEP_ARCHIVE  |  GLACIER_IR
	Endpoint     string
}

type S3 struct {
	*S3Config
	client     *s3.S3
	downloader *s3manager.Downloader
	uploader   *s3manager.Uploader
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

	sessionOptions := session.Options{}
	awsConfig := aws.Config{}

	if config.Region != "" {
		awsConfig.WithRegion(config.Region)
	}

	if config.Access != "" {
		awsConfig.WithCredentials(credentials.NewStaticCredentials(config.Access, config.Secret, ""))
	}

	if config.Endpoint != "" {
		awsConfig.WithEndpoint(config.Endpoint)
	}

	if config.StorageClass != "" {
		validValues := s3.StorageClass_Values()

		if !slices.Contains(validValues, config.StorageClass) {
			return nil, fmt.Errorf(errorMessages.EnumError, "cache.s3.storageclass", config.StorageClass, validValues)
		}

		if strings.Contains(config.StorageClass, "ONEZONE") {
			//Directory AKA Express One Zone fails if an MD5 header set. The Go SDK requires you find this obscure flag to disable that
			awsConfig.WithS3DisableContentMD5Validation(true)
		}
	}

	sessionOptions.Config = awsConfig

	if config.Profile != "" {
		sessionOptions.Profile = config.Profile
	}

	awsSession, err := session.NewSessionWithOptions(sessionOptions)

	if err != nil {
		return nil, err
	}

	s3Client := s3.New(awsSession, &awsConfig)
	downloader := s3manager.NewDownloader(awsSession)
	uploader := s3manager.NewUploader(awsSession, s3manager.WithUploaderRequestOptions())

	return &S3{config, s3Client, downloader, uploader}, nil
}

func calcKey(config *S3, t *internal.TileRequest) string {
	return config.Path + t.LayerName + "/" + strconv.Itoa(t.Z) + "/" + strconv.Itoa(t.X) + "/" + strconv.Itoa(t.Y)
}

// Just for testing purposes
func (c S3) makeBucket() error {
	_, err := c.client.CreateBucket(&s3.CreateBucketInput{Bucket: &c.Bucket})
	return err
}

func (c S3) Lookup(t internal.TileRequest) (*internal.Image, error) {
	writer := aws.NewWriteAtBuffer([]byte{})

	_, err := c.downloader.Download(
		writer,
		&s3.GetObjectInput{
			Bucket: aws.String(c.Bucket),
			Key:    aws.String(calcKey(&c, &t)),
		})

	if err != nil {
		var requestFailure awserr.Error
		if errors.As(err, &requestFailure) && requestFailure.Code() == s3.ErrCodeNoSuchKey {
			//Simple cache miss
			return nil, nil
		}

		return nil, err
	}

	img := internal.Image(writer.Bytes())

	return &img, nil
}

func (c S3) Save(t internal.TileRequest, img *internal.Image) error {

	uploadConfig := &s3manager.UploadInput{
		Bucket: &c.Bucket,
		Key:    aws.String(calcKey(&c, &t)),
		Body:   bytes.NewReader(*img),
	}

	if c.StorageClass != "" {
		uploadConfig.StorageClass = &c.StorageClass
	}

	_, err := c.uploader.Upload(uploadConfig)
	return err
}
