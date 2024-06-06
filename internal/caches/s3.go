package caches

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/internal/config"
	"github.com/Michad/tilegroxy/pkg"
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

	downloader := s3manager.NewDownloader(awsSession)
	uploader := s3manager.NewUploader(awsSession, s3manager.WithUploaderRequestOptions())

	return &S3{config, downloader, uploader}, nil
}

func calcKey(config *S3, t *pkg.TileRequest) string {
	return config.Path + t.LayerName + "/" + strconv.Itoa(t.Z) + "/" + strconv.Itoa(t.X) + "/" + strconv.Itoa(t.Y)
}

func (c S3) Lookup(t pkg.TileRequest) (*pkg.Image, error) {
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

	img := pkg.Image(writer.Bytes())

	return &img, nil
}

func (c S3) Save(t pkg.TileRequest, img *pkg.Image) error {

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
