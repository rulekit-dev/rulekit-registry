package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rulekit/rulekit-registry/internal/model"
	"github.com/rulekit/rulekit-registry/internal/store"
)

const schema = `
CREATE TABLE IF NOT EXISTS rulesets (
    namespace   TEXT NOT NULL,
    key         TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (namespace, key)
);
CREATE TABLE IF NOT EXISTS drafts (
    namespace   TEXT NOT NULL,
    ruleset_key TEXT NOT NULL,
    dsl         TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (namespace, ruleset_key)
);
CREATE TABLE IF NOT EXISTS versions (
    namespace   TEXT NOT NULL,
    ruleset_key TEXT NOT NULL,
    version     INTEGER NOT NULL,
    checksum    TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (namespace, ruleset_key, version)
);`

type SQLiteStore struct {
	db *sql.DB
}

func New(dataDir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("sqlite: create data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "rulekit.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: migrate: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) ListRulesets(ctx context.Context, namespace string) ([]*model.Ruleset, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT namespace, key, name, description, created_at, updated_at
         FROM rulesets WHERE namespace = ? ORDER BY key`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Ruleset
	for rows.Next() {
		r := &model.Ruleset{}
		var ca, ua string
		if err := rows.Scan(&r.Namespace, &r.Key, &r.Name, &r.Description, &ca, &ua); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, ua)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CreateRuleset(ctx context.Context, r *model.Ruleset) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rulesets (namespace, key, name, description, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
		r.Namespace, r.Key, r.Name, r.Description,
		r.CreatedAt.UTC().Format(time.RFC3339Nano),
		r.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return store.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *SQLiteStore) GetRuleset(ctx context.Context, namespace, key string) (*model.Ruleset, error) {
	r := &model.Ruleset{}
	var ca, ua string
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, key, name, description, created_at, updated_at
         FROM rulesets WHERE namespace = ? AND key = ?`, namespace, key).
		Scan(&r.Namespace, &r.Key, &r.Name, &r.Description, &ca, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, ua)
	return r, nil
}

func (s *SQLiteStore) GetDraft(ctx context.Context, namespace, key string) (*model.Draft, error) {
	d := &model.Draft{}
	var dslStr, ua string
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, dsl, updated_at
         FROM drafts WHERE namespace = ? AND ruleset_key = ?`, namespace, key).
		Scan(&d.Namespace, &d.RulesetKey, &dslStr, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.DSL = []byte(dslStr)
	d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, ua)
	return d, nil
}

func (s *SQLiteStore) UpsertDraft(ctx context.Context, d *model.Draft) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO drafts (namespace, ruleset_key, dsl, updated_at)
         VALUES (?, ?, ?, ?)`,
		d.Namespace, d.RulesetKey, string(d.DSL),
		d.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) ListVersions(ctx context.Context, namespace, key string) ([]*model.Version, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = ? AND ruleset_key = ? ORDER BY version ASC`,
		namespace, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Version
	for rows.Next() {
		v := &model.Version{}
		var ca string
		if err := rows.Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca); err != nil {
			return nil, err
		}
		v.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetVersion(ctx context.Context, namespace, key string, version int) (*model.Version, error) {
	v := &model.Version{}
	var ca string
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = ? AND ruleset_key = ? AND version = ?`,
		namespace, key, version).
		Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	return v, nil
}

func (s *SQLiteStore) GetLatestVersion(ctx context.Context, namespace, key string) (*model.Version, error) {
	v := &model.Version{}
	var ca string
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = ? AND ruleset_key = ?
         ORDER BY version DESC LIMIT 1`, namespace, key).
		Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	return v, nil
}

func (s *SQLiteStore) CreateVersion(ctx context.Context, v *model.Version) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM versions WHERE namespace = ? AND ruleset_key = ? AND version = ?`,
		v.Namespace, v.RulesetKey, v.Version).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return store.ErrVersionImmutable
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO versions (namespace, ruleset_key, version, checksum, created_at)
         VALUES (?, ?, ?, ?, ?)`,
		v.Namespace, v.RulesetKey, v.Version, v.Checksum,
		v.CreatedAt.UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) NextVersionNumber(ctx context.Context, namespace, key string) (int, error) {
	var maxVer sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(version) FROM versions WHERE namespace = ? AND ruleset_key = ?`,
		namespace, key).Scan(&maxVer)
	if err != nil {
		return 0, err
	}
	if !maxVer.Valid {
		return 1, nil
	}
	return int(maxVer.Int64) + 1, nil
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
