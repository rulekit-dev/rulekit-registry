package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// AuthMode controls authentication behaviour.
//   - "none" (default) — legacy single-API-key mode; RULEKIT_API_KEY still applies
//   - "jwt"  — full email+OTP login with JWT sessions and RBAC
type AuthMode string

const (
	AuthModeNone AuthMode = "none"
	AuthModeJWT  AuthMode = "jwt"
)

type Config struct {
	Addr        string // RULEKIT_ADDR
	DataDir     string // RULEKIT_DATA_DIR  (SQLite file directory)
	Store       string // RULEKIT_STORE     (sqlite | postgres)
	DatabaseURL string // RULEKIT_DATABASE_URL

	// Auth — legacy single-key mode (AuthMode=none)
	APIKey string // RULEKIT_API_KEY

	// Auth — JWT mode (AuthMode=jwt)
	AuthMode   AuthMode // RULEKIT_AUTH: "none" (default) | "jwt"
	JWTSecret  string   // RULEKIT_JWT_SECRET: required when auth=jwt
	AdminEmail string   // RULEKIT_ADMIN_EMAIL: bootstrapped superadmin email

	// SMTP — optional; if unset OTP codes are printed to stdout
	SMTPHost     string // RULEKIT_SMTP_HOST
	SMTPPort     int    // RULEKIT_SMTP_PORT (default: 587)
	SMTPUsername string // RULEKIT_SMTP_USERNAME
	SMTPPassword string // RULEKIT_SMTP_PASSWORD
	SMTPFrom     string // RULEKIT_SMTP_FROM
	SMTPUseTLS   bool   // RULEKIT_SMTP_USE_TLS: use implicit TLS (port 465) instead of STARTTLS

	// CORS
	CORSOrigins string // RULEKIT_CORS_ORIGINS: comma-separated allowed origins, "*" for all

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
	flag.StringVar(&cfg.APIKey, "api-key", env("RULEKIT_API_KEY", ""), "API key for bearer token auth (env: RULEKIT_API_KEY)")

	authMode := flag.String("auth", env("RULEKIT_AUTH", "none"), "auth mode: none or jwt (env: RULEKIT_AUTH)")
	flag.StringVar(&cfg.JWTSecret, "jwt-secret", env("RULEKIT_JWT_SECRET", ""), "JWT signing secret (env: RULEKIT_JWT_SECRET)")
	flag.StringVar(&cfg.AdminEmail, "admin-email", env("RULEKIT_ADMIN_EMAIL", ""), "bootstrap admin email (env: RULEKIT_ADMIN_EMAIL)")

	flag.StringVar(&cfg.SMTPHost, "smtp-host", env("RULEKIT_SMTP_HOST", ""), "SMTP host (env: RULEKIT_SMTP_HOST)")
	smtpPort := flag.Int("smtp-port", envInt("RULEKIT_SMTP_PORT", 587), "SMTP port (env: RULEKIT_SMTP_PORT)")
	flag.StringVar(&cfg.SMTPUsername, "smtp-username", env("RULEKIT_SMTP_USERNAME", ""), "SMTP username (env: RULEKIT_SMTP_USERNAME)")
	flag.StringVar(&cfg.SMTPPassword, "smtp-password", env("RULEKIT_SMTP_PASSWORD", ""), "SMTP password (env: RULEKIT_SMTP_PASSWORD)")
	flag.StringVar(&cfg.SMTPFrom, "smtp-from", env("RULEKIT_SMTP_FROM", ""), "SMTP from address (env: RULEKIT_SMTP_FROM)")
	smtpTLS := flag.Bool("smtp-use-tls", envBool("RULEKIT_SMTP_USE_TLS", false), "use implicit TLS for SMTP (env: RULEKIT_SMTP_USE_TLS)")

	flag.StringVar(&cfg.CORSOrigins, "cors-origins", env("RULEKIT_CORS_ORIGINS", ""), "allowed CORS origins, comma-separated or * (env: RULEKIT_CORS_ORIGINS)")

	flag.StringVar(&cfg.BlobStore, "blob-store", env("RULEKIT_BLOB_STORE", "fs"), "blob store backend: fs or s3 (env: RULEKIT_BLOB_STORE)")
	flag.StringVar(&cfg.BlobDir, "blob-dir", env("RULEKIT_BLOB_DIR", ""), "directory for fs blob store (env: RULEKIT_BLOB_DIR)")
	flag.StringVar(&cfg.S3Bucket, "s3-bucket", env("RULEKIT_S3_BUCKET", ""), "S3 bucket name (env: RULEKIT_S3_BUCKET)")
	flag.StringVar(&cfg.S3Endpoint, "s3-endpoint", env("RULEKIT_S3_ENDPOINT", ""), "S3 custom endpoint, e.g. for R2 (env: RULEKIT_S3_ENDPOINT)")
	flag.StringVar(&cfg.S3Region, "s3-region", env("RULEKIT_S3_REGION", "us-east-1"), "S3 region (env: RULEKIT_S3_REGION)")
	flag.StringVar(&cfg.S3AccessKeyID, "s3-access-key-id", env("RULEKIT_S3_ACCESS_KEY_ID", ""), "S3 access key ID (env: RULEKIT_S3_ACCESS_KEY_ID)")
	flag.StringVar(&cfg.S3SecretKey, "s3-secret-access-key", env("RULEKIT_S3_SECRET_ACCESS_KEY", ""), "S3 secret access key (env: RULEKIT_S3_SECRET_ACCESS_KEY)")

	flag.Parse()

	cfg.AuthMode = AuthMode(*authMode)
	cfg.SMTPPort = *smtpPort
	cfg.SMTPUseTLS = *smtpTLS

	if cfg.BlobDir == "" {
		cfg.BlobDir = filepath.Join(cfg.DataDir, "blobs")
	}

	if cfg.Store != "sqlite" && cfg.Store != "postgres" {
		return nil, fmt.Errorf("config: unknown store %q (must be sqlite or postgres)", cfg.Store)
	}
	if cfg.Store == "postgres" && cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("config: RULEKIT_DATABASE_URL is required when store=postgres")
	}

	if cfg.AuthMode != AuthModeNone && cfg.AuthMode != AuthModeJWT {
		return nil, fmt.Errorf("config: unknown auth mode %q (must be none or jwt)", cfg.AuthMode)
	}
	if cfg.AuthMode == AuthModeJWT && cfg.JWTSecret == "" {
		return nil, fmt.Errorf("config: RULEKIT_JWT_SECRET is required when auth=jwt")
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

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return fallback
}
