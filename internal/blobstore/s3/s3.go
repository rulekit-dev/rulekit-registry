package s3blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/rulekit/rulekit-registry/internal/blobstore"
)

type Config struct {
	Bucket          string
	Endpoint        string // custom endpoint for R2 or other S3-compatible services
	Region          string // use "auto" for Cloudflare R2
	AccessKeyID     string
	SecretAccessKey string
}

type S3BlobStore struct {
	client *s3.Client
	bucket string
}

func New(cfg Config) (*S3BlobStore, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("blobstore/s3: bucket must not be empty")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if cfg.AccessKeyID != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("blobstore/s3: load aws config: %w", err)
	}

	s3Opts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			// Path-style addressing required for some S3-compatible services.
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)
	return &S3BlobStore{client: client, bucket: cfg.Bucket}, nil
}

func (b *S3BlobStore) dslKey(namespace, key string, version int) string {
	return fmt.Sprintf("%s/%s/v%d/dsl.json", namespace, key, version)
}

func (b *S3BlobStore) bundleKey(namespace, key string, version int) string {
	return fmt.Sprintf("%s/%s/v%d/bundle.zip", namespace, key, version)
}

func (b *S3BlobStore) put(ctx context.Context, objKey, contentType string, data []byte) error {
	_, err := b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(b.bucket),
		Key:         aws.String(objKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("blobstore/s3: put %s: %w", objKey, err)
	}
	return nil
}

func (b *S3BlobStore) get(ctx context.Context, objKey string) ([]byte, error) {
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(objKey),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, blobstore.ErrNotFound
		}
		return nil, fmt.Errorf("blobstore/s3: get %s: %w", objKey, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("blobstore/s3: read body %s: %w", objKey, err)
	}
	return data, nil
}

func (b *S3BlobStore) PutDSL(ctx context.Context, namespace, key string, version int, data []byte) error {
	return b.put(ctx, b.dslKey(namespace, key, version), "application/json", data)
}

func (b *S3BlobStore) GetDSL(ctx context.Context, namespace, key string, version int) ([]byte, error) {
	return b.get(ctx, b.dslKey(namespace, key, version))
}

func (b *S3BlobStore) PutBundle(ctx context.Context, namespace, key string, version int, data []byte) error {
	return b.put(ctx, b.bundleKey(namespace, key, version), "application/zip", data)
}

func (b *S3BlobStore) GetBundle(ctx context.Context, namespace, key string, version int) ([]byte, error) {
	return b.get(ctx, b.bundleKey(namespace, key, version))
}

func (b *S3BlobStore) Close() error { return nil }

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound"
	}
	return false
}
