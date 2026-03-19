package s3blob_test

import (
	"os"
	"testing"

	"github.com/rulekit/rulekit-registry/internal/blobstore"
	s3blob "github.com/rulekit/rulekit-registry/internal/blobstore/s3"
	"github.com/rulekit/rulekit-registry/internal/blobstore/testhelper"
)

func TestS3BlobStore(t *testing.T) {
	bucket := os.Getenv("RULEKIT_S3_TEST_BUCKET")
	if bucket == "" {
		t.Skip("skipping S3 blobstore tests: RULEKIT_S3_TEST_BUCKET not set")
	}

	testhelper.RunSuite(t, func(t *testing.T) blobstore.BlobStore {
		b, err := s3blob.New(s3blob.Config{
			Bucket:          bucket,
			Endpoint:        os.Getenv("RULEKIT_S3_TEST_ENDPOINT"),
			Region:          os.Getenv("RULEKIT_S3_TEST_REGION"),
			AccessKeyID:     os.Getenv("RULEKIT_S3_TEST_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("RULEKIT_S3_TEST_SECRET_ACCESS_KEY"),
		})
		if err != nil {
			t.Fatalf("s3blob.New: %v", err)
		}
		t.Cleanup(func() { b.Close() })
		return b
	})
}
