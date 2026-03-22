package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpadapter "github.com/rulekit-dev/rulekit-registry/internal/adapter/http"
	"github.com/rulekit-dev/rulekit-registry/internal/adapter/http/handler"
	fsblobstore "github.com/rulekit-dev/rulekit-registry/internal/adapter/blob/fs"
	s3blob "github.com/rulekit-dev/rulekit-registry/internal/adapter/blob/s3"
	"github.com/rulekit-dev/rulekit-registry/internal/adapter/mailer"
	postgresstore "github.com/rulekit-dev/rulekit-registry/internal/adapter/store/postgres"
	sqlitestore "github.com/rulekit-dev/rulekit-registry/internal/adapter/store/sqlite"
	"github.com/rulekit-dev/rulekit-registry/internal/config"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
	"github.com/rulekit-dev/rulekit-registry/internal/service"
)

func main() {
	startTime := time.Now()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	var db port.Datastore

	switch cfg.Store {
	case "sqlite":
		slog.Info("using sqlite store", "data_dir", cfg.DataDir)
		sqliteStore, err := sqlitestore.New(cfg.DataDir)
		if err != nil {
			slog.Error("failed to initialise sqlite store", "error", err)
			os.Exit(1)
		}
		db = sqliteStore
	case "postgres":
		slog.Info("using postgres store", "database_url", cfg.DatabaseURL)
		pgStore, err := postgresstore.New(cfg.DatabaseURL)
		if err != nil {
			slog.Error("failed to initialise postgres store", "error", err)
			os.Exit(1)
		}
		db = pgStore
	default:
		slog.Error("unknown store backend", "store", cfg.Store)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("error closing store", "error", err)
		}
	}()

	var blobs port.BlobStore
	switch cfg.BlobStore {
	case "fs":
		slog.Info("using fs blob store", "blob_dir", cfg.BlobDir)
		blobs, err = fsblobstore.New(cfg.BlobDir)
	case "s3":
		slog.Info("using s3 blob store", "bucket", cfg.S3Bucket)
		blobs, err = s3blob.New(s3blob.Config{
			Bucket:          cfg.S3Bucket,
			Endpoint:        cfg.S3Endpoint,
			Region:          cfg.S3Region,
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretKey,
		})
	default:
		slog.Error("unknown blob store backend", "blob_store", cfg.BlobStore)
		os.Exit(1)
	}
	if err != nil {
		slog.Error("failed to initialize blob store", "error", err)
		os.Exit(1)
	}
	defer blobs.Close()

	var m port.Mailer
	if cfg.SMTPHost != "" {
		m = mailer.NewSMTP(mailer.SMTPConfig{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
			UseTLS:   cfg.SMTPUseTLS,
		})
		slog.Info("using SMTP mailer", "host", cfg.SMTPHost)
	} else {
		m = mailer.NewStdout()
		slog.Info("no SMTP configured — OTP codes will be printed to stdout")
	}

	rulesetSvc := service.NewRulesetService(db, blobs)
	authSvc := service.NewAuthService(db, m, []byte(cfg.JWTSecret), cfg.AdminPassword)
	adminSvc := service.NewAdminService(db)
	workspaceSvc := service.NewWorkspaceService(db)

	rulesetHandler := handler.NewRulesetHandler(rulesetSvc)
	authHandler := handler.NewAuthHandler(authSvc)
	adminHandler := handler.NewAdminHandler(adminSvc)
	workspaceHandler := handler.NewWorkspaceHandler(workspaceSvc)

	httpHandler := httpadapter.NewRouter(rulesetHandler, authHandler, adminHandler, workspaceHandler, db, cfg, startTime)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      httpHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("rulekitd starting", "addr", cfg.Addr, "store", cfg.Store)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErr:
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	slog.Info("shutting down server gracefully")
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
