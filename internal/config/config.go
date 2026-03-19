package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Addr        string // RULEKIT_ADDR
	DataDir     string // RULEKIT_DATA_DIR  (SQLite file directory)
	Store       string // RULEKIT_STORE     (sqlite | postgres)
	DatabaseURL string // RULEKIT_DATABASE_URL

	// Blob store
	BlobStore     string // RULEKIT_BLOB_STORE: "fs" (default) | "s3"
	BlobDir       string // RULEKIT_BLOB_DIR: dir for fs backend (default: {DataDir}/blobs)
	S3Bucket      string // RULEKIT_S3_BUCKET
	S3Endpoint    string // RULEKIT_S3_ENDPOINT (custom endpoint for R2, etc.)
	S3Region      string // RULEKIT_S3_REGION (default: "us-east-1", use "auto" for R2)
	S3AccessKeyID string // RULEKIT_S3_ACCESS_KEY_ID
	S3SecretKey   string // RULEKIT_S3_SECRET_ACCESS_KEY
}

func Load() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.Addr, "addr", env("RULEKIT_ADDR", ":8080"), "listen address (env: RULEKIT_ADDR)")
	flag.StringVar(&cfg.DataDir, "data-dir", env("RULEKIT_DATA_DIR", "./data"), "data directory for SQLite (env: RULEKIT_DATA_DIR)")
	flag.StringVar(&cfg.Store, "store", env("RULEKIT_STORE", "sqlite"), "storage backend: sqlite or postgres (env: RULEKIT_STORE)")
	flag.StringVar(&cfg.DatabaseURL, "database-url", env("RULEKIT_DATABASE_URL", ""), "PostgreSQL DSN (env: RULEKIT_DATABASE_URL)")

	flag.StringVar(&cfg.BlobStore, "blob-store", env("RULEKIT_BLOB_STORE", "fs"), "blob store backend: fs or s3 (env: RULEKIT_BLOB_STORE)")
	flag.StringVar(&cfg.BlobDir, "blob-dir", env("RULEKIT_BLOB_DIR", ""), "directory for fs blob store (env: RULEKIT_BLOB_DIR)")
	flag.StringVar(&cfg.S3Bucket, "s3-bucket", env("RULEKIT_S3_BUCKET", ""), "S3 bucket name (env: RULEKIT_S3_BUCKET)")
	flag.StringVar(&cfg.S3Endpoint, "s3-endpoint", env("RULEKIT_S3_ENDPOINT", ""), "S3 custom endpoint, e.g. for R2 (env: RULEKIT_S3_ENDPOINT)")
	flag.StringVar(&cfg.S3Region, "s3-region", env("RULEKIT_S3_REGION", "us-east-1"), "S3 region (env: RULEKIT_S3_REGION)")
	flag.StringVar(&cfg.S3AccessKeyID, "s3-access-key-id", env("RULEKIT_S3_ACCESS_KEY_ID", ""), "S3 access key ID (env: RULEKIT_S3_ACCESS_KEY_ID)")
	flag.StringVar(&cfg.S3SecretKey, "s3-secret-access-key", env("RULEKIT_S3_SECRET_ACCESS_KEY", ""), "S3 secret access key (env: RULEKIT_S3_SECRET_ACCESS_KEY)")

	flag.Parse()

	// Apply DataDir-relative default for BlobDir after flag parsing.
	if cfg.BlobDir == "" {
		cfg.BlobDir = filepath.Join(cfg.DataDir, "blobs")
	}

	if cfg.Store != "sqlite" && cfg.Store != "postgres" {
		return nil, fmt.Errorf("config: unknown store %q (must be sqlite or postgres)", cfg.Store)
	}
	if cfg.Store == "postgres" && cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: RULEKIT_DATABASE_URL is required when store=postgres")
	}

	if cfg.BlobStore != "fs" && cfg.BlobStore != "s3" {
		return nil, fmt.Errorf("config: unknown blob store %q (must be fs or s3)", cfg.BlobStore)
	}
	if cfg.BlobStore == "s3" && cfg.S3Bucket == "" {
		return nil, fmt.Errorf("config: RULEKIT_S3_BUCKET is required when blob-store=s3")
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
