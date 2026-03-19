package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/rulekit/rulekit-registry/internal/model"
	"github.com/rulekit/rulekit-registry/internal/store"
)

const schema = `
CREATE TABLE IF NOT EXISTS rulesets (
    namespace   TEXT        NOT NULL,
    key         TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (namespace, key)
);
CREATE TABLE IF NOT EXISTS drafts (
    namespace   TEXT        NOT NULL,
    ruleset_key TEXT        NOT NULL,
    dsl         JSONB       NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (namespace, ruleset_key)
);
CREATE TABLE IF NOT EXISTS versions (
    namespace   TEXT        NOT NULL,
    ruleset_key TEXT        NOT NULL,
    version     INTEGER     NOT NULL,
    checksum    TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (namespace, ruleset_key, version)
);`

type PostgresStore struct {
	db *sql.DB
}

func New(databaseURL string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres: migrate: %w", err)
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Close() error { return s.db.Close() }

func (s *PostgresStore) ListRulesets(ctx context.Context, namespace string, limit, offset int) ([]*model.Ruleset, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT namespace, key, name, description, created_at, updated_at
         FROM rulesets WHERE namespace = $1 ORDER BY key LIMIT $2 OFFSET $3`, namespace, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Ruleset
	for rows.Next() {
		r := &model.Ruleset{}
		if err := rows.Scan(&r.Namespace, &r.Key, &r.Name, &r.Description, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.CreatedAt = r.CreatedAt.UTC()
		r.UpdatedAt = r.UpdatedAt.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateRuleset(ctx context.Context, r *model.Ruleset) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rulesets (namespace, key, name, description, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6)`,
		r.Namespace, r.Key, r.Name, r.Description,
		r.CreatedAt.UTC(), r.UpdatedAt.UTC(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *PostgresStore) GetRuleset(ctx context.Context, namespace, key string) (*model.Ruleset, error) {
	r := &model.Ruleset{}
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, key, name, description, created_at, updated_at
         FROM rulesets WHERE namespace = $1 AND key = $2`, namespace, key).
		Scan(&r.Namespace, &r.Key, &r.Name, &r.Description, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return r, nil
}

func (s *PostgresStore) GetDraft(ctx context.Context, namespace, key string) (*model.Draft, error) {
	d := &model.Draft{}
	var ua time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, dsl, updated_at
         FROM drafts WHERE namespace = $1 AND ruleset_key = $2`, namespace, key).
		Scan(&d.Namespace, &d.RulesetKey, &d.DSL, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.UpdatedAt = ua.UTC()
	return d, nil
}

func (s *PostgresStore) UpsertDraft(ctx context.Context, d *model.Draft) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO drafts (namespace, ruleset_key, dsl, updated_at)
         VALUES ($1, $2, $3, $4)
         ON CONFLICT (namespace, ruleset_key)
         DO UPDATE SET dsl = EXCLUDED.dsl, updated_at = EXCLUDED.updated_at`,
		d.Namespace, d.RulesetKey, d.DSL, d.UpdatedAt.UTC(),
	)
	return err
}

func (s *PostgresStore) DeleteDraft(ctx context.Context, namespace, key string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM drafts WHERE namespace = $1 AND ruleset_key = $2`, namespace, key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteRuleset(ctx context.Context, namespace, key string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM rulesets WHERE namespace = $1 AND key = $2`, namespace, key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListVersions(ctx context.Context, namespace, key string, limit, offset int) ([]*model.Version, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = $1 AND ruleset_key = $2 ORDER BY version ASC LIMIT $3 OFFSET $4`,
		namespace, key, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Version
	for rows.Next() {
		v := &model.Version{}
		var ca time.Time
		if err := rows.Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca); err != nil {
			return nil, err
		}
		v.CreatedAt = ca.UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetVersion(ctx context.Context, namespace, key string, version int) (*model.Version, error) {
	v := &model.Version{}
	var ca time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = $1 AND ruleset_key = $2 AND version = $3`,
		namespace, key, version).
		Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.CreatedAt = ca.UTC()
	return v, nil
}

func (s *PostgresStore) GetLatestVersion(ctx context.Context, namespace, key string) (*model.Version, error) {
	v := &model.Version{}
	var ca time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = $1 AND ruleset_key = $2
         ORDER BY version DESC LIMIT 1`, namespace, key).
		Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.CreatedAt = ca.UTC()
	return v, nil
}

func (s *PostgresStore) CreateVersion(ctx context.Context, v *model.Version) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM versions WHERE namespace = $1 AND ruleset_key = $2 AND version = $3`,
		v.Namespace, v.RulesetKey, v.Version).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return store.ErrVersionImmutable
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO versions (namespace, ruleset_key, version, checksum, created_at)
         VALUES ($1, $2, $3, $4, $5)`,
		v.Namespace, v.RulesetKey, v.Version, v.Checksum,
		v.CreatedAt.UTC(),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) NextVersionNumber(ctx context.Context, namespace, key string) (int, error) {
	var maxVer sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(version) FROM versions WHERE namespace = $1 AND ruleset_key = $2`,
		namespace, key).Scan(&maxVer)
	if err != nil {
		return 0, err
	}
	if !maxVer.Valid {
		return 1, nil
	}
	return int(maxVer.Int64) + 1, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps the error; check for the SQLSTATE code in the message.
	return strings.Contains(err.Error(), "23505") ||
		strings.Contains(err.Error(), "unique constraint") ||
		strings.Contains(err.Error(), "duplicate key")
}
