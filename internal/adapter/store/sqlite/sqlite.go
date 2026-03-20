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

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
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
);
CREATE TABLE IF NOT EXISTS users (
    id            TEXT NOT NULL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    created_at    TEXT NOT NULL,
    last_login_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS otp_codes (
    id         TEXT NOT NULL PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_otp_codes_user_id ON otp_codes(user_id);
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         TEXT NOT NULL PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    revoked_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);
CREATE TABLE IF NOT EXISTS api_keys (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL,
    key_hash   TEXT NOT NULL UNIQUE,
    namespace  TEXT NOT NULL,
    role       INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT,
    revoked_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);
CREATE TABLE IF NOT EXISTS user_roles (
    user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    namespace TEXT NOT NULL,
    role_mask INTEGER NOT NULL,
    PRIMARY KEY (user_id, namespace)
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

func (s *SQLiteStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) ListRulesets(ctx context.Context, namespace string, limit, offset int) ([]*domain.Ruleset, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT namespace, key, name, description, created_at, updated_at
         FROM rulesets WHERE namespace = ? ORDER BY key LIMIT ? OFFSET ?`, namespace, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Ruleset
	for rows.Next() {
		r := &domain.Ruleset{}
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

func (s *SQLiteStore) CreateRuleset(ctx context.Context, r *domain.Ruleset) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rulesets (namespace, key, name, description, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
		r.Namespace, r.Key, r.Name, r.Description,
		r.CreatedAt.UTC().Format(time.RFC3339Nano),
		r.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return port.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *SQLiteStore) GetRuleset(ctx context.Context, namespace, key string) (*domain.Ruleset, error) {
	r := &domain.Ruleset{}
	var ca, ua string
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, key, name, description, created_at, updated_at
         FROM rulesets WHERE namespace = ? AND key = ?`, namespace, key).
		Scan(&r.Namespace, &r.Key, &r.Name, &r.Description, &ca, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, ua)
	return r, nil
}

func (s *SQLiteStore) GetDraft(ctx context.Context, namespace, key string) (*domain.Draft, error) {
	d := &domain.Draft{}
	var dslStr, ua string
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, dsl, updated_at
         FROM drafts WHERE namespace = ? AND ruleset_key = ?`, namespace, key).
		Scan(&d.Namespace, &d.RulesetKey, &dslStr, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.DSL = []byte(dslStr)
	d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, ua)
	return d, nil
}

func (s *SQLiteStore) UpsertDraft(ctx context.Context, d *domain.Draft) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO drafts (namespace, ruleset_key, dsl, updated_at)
         VALUES (?, ?, ?, ?)`,
		d.Namespace, d.RulesetKey, string(d.DSL),
		d.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) DeleteDraft(ctx context.Context, namespace, key string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM drafts WHERE namespace = ? AND ruleset_key = ?`, namespace, key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return port.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteRuleset(ctx context.Context, namespace, key string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM rulesets WHERE namespace = ? AND key = ?`, namespace, key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return port.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ListVersions(ctx context.Context, namespace, key string, limit, offset int) ([]*domain.Version, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = ? AND ruleset_key = ? ORDER BY version ASC LIMIT ? OFFSET ?`,
		namespace, key, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Version
	for rows.Next() {
		v := &domain.Version{}
		var ca string
		if err := rows.Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca); err != nil {
			return nil, err
		}
		v.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetVersion(ctx context.Context, namespace, key string, version int) (*domain.Version, error) {
	v := &domain.Version{}
	var ca string
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = ? AND ruleset_key = ? AND version = ?`,
		namespace, key, version).
		Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	return v, nil
}

func (s *SQLiteStore) GetLatestVersion(ctx context.Context, namespace, key string) (*domain.Version, error) {
	v := &domain.Version{}
	var ca string
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = ? AND ruleset_key = ?
         ORDER BY version DESC LIMIT 1`, namespace, key).
		Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	return v, nil
}

func (s *SQLiteStore) CreateVersion(ctx context.Context, v *domain.Version) error {
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
		return port.ErrVersionImmutable
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

// --- User ---

func (s *SQLiteStore) CreateUser(ctx context.Context, u *domain.User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, created_at, last_login_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Email,
		u.CreatedAt.UTC().Format(time.RFC3339Nano),
		u.LastLoginAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return port.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *SQLiteStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	u := &domain.User{}
	var ca, lla string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, created_at, last_login_at FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &ca, &lla)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	u.LastLoginAt, _ = time.Parse(time.RFC3339Nano, lla)
	return u, nil
}

func (s *SQLiteStore) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	u := &domain.User{}
	var ca, lla string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, created_at, last_login_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Email, &ca, &lla)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	u.LastLoginAt, _ = time.Parse(time.RFC3339Nano, lla)
	return u, nil
}

func (s *SQLiteStore) UpdateUserLastLogin(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), userID)
	return err
}

func (s *SQLiteStore) ListUsers(ctx context.Context, limit, offset int) ([]*domain.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, created_at, last_login_at FROM users ORDER BY email LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.User
	for rows.Next() {
		u := &domain.User{}
		var ca, lla string
		if err := rows.Scan(&u.ID, &u.Email, &ca, &lla); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
		u.LastLoginAt, _ = time.Parse(time.RFC3339Nano, lla)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteUser(ctx context.Context, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return port.ErrNotFound
	}
	return nil
}

// --- OTP ---

func (s *SQLiteStore) CreateOTPCode(ctx context.Context, otp *domain.OTPCode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO otp_codes (id, user_id, code_hash, expires_at) VALUES (?, ?, ?, ?)`,
		otp.ID, otp.UserID, otp.CodeHash,
		otp.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) GetUnusedOTPCode(ctx context.Context, userID string) (*domain.OTPCode, error) {
	otp := &domain.OTPCode{}
	var exp string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, code_hash, expires_at FROM otp_codes
         WHERE user_id = ? AND used_at IS NULL AND expires_at > ?
         ORDER BY expires_at DESC LIMIT 1`,
		userID, time.Now().UTC().Format(time.RFC3339Nano)).
		Scan(&otp.ID, &otp.UserID, &otp.CodeHash, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	otp.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
	return otp, nil
}

func (s *SQLiteStore) MarkOTPUsed(ctx context.Context, otpID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE otp_codes SET used_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), otpID)
	return err
}

func (s *SQLiteStore) DeleteExpiredOTPs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM otp_codes WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// --- Refresh token ---

func (s *SQLiteStore) CreateRefreshToken(ctx context.Context, t *domain.RefreshToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		t.ID, t.UserID, t.TokenHash,
		t.ExpiresAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	t := &domain.RefreshToken{}
	var exp string
	var rev sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash).Scan(&t.ID, &t.UserID, &t.TokenHash, &exp, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
	if rev.Valid {
		tt, _ := time.Parse(time.RFC3339Nano, rev.String)
		t.RevokedAt = &tt
	}
	return t, nil
}

func (s *SQLiteStore) RevokeRefreshToken(ctx context.Context, tokenID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), tokenID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return port.ErrNotFound
	}
	return nil
}

// --- API key ---

func (s *SQLiteStore) CreateAPIKey(ctx context.Context, k *domain.APIKey) error {
	var exp interface{}
	if k.ExpiresAt != nil {
		exp = k.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, name, key_hash, namespace, role, created_at, expires_at)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Name, k.KeyHash, k.Namespace, int(k.Role),
		k.CreatedAt.UTC().Format(time.RFC3339Nano), exp,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return port.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *SQLiteStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	k := &domain.APIKey{}
	var ca, exp, rev sql.NullString
	var role int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, key_hash, namespace, role, created_at, expires_at, revoked_at
         FROM api_keys WHERE key_hash = ?`, keyHash).
		Scan(&k.ID, &k.Name, &k.KeyHash, &k.Namespace, &role, &ca, &exp, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	k.Role = domain.Role(role)
	k.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca.String)
	if exp.Valid {
		tt, _ := time.Parse(time.RFC3339Nano, exp.String)
		k.ExpiresAt = &tt
	}
	if rev.Valid {
		tt, _ := time.Parse(time.RFC3339Nano, rev.String)
		k.RevokedAt = &tt
	}
	return k, nil
}

func (s *SQLiteStore) ListAPIKeys(ctx context.Context, limit, offset int) ([]*domain.APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, key_hash, namespace, role, created_at, expires_at, revoked_at
         FROM api_keys ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.APIKey
	for rows.Next() {
		k := &domain.APIKey{}
		var ca, exp, rev sql.NullString
		var role int
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyHash, &k.Namespace, &role, &ca, &exp, &rev); err != nil {
			return nil, err
		}
		k.Role = domain.Role(role)
		k.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca.String)
		if exp.Valid {
			tt, _ := time.Parse(time.RFC3339Nano, exp.String)
			k.ExpiresAt = &tt
		}
		if rev.Valid {
			tt, _ := time.Parse(time.RFC3339Nano, rev.String)
			k.RevokedAt = &tt
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) RevokeAPIKey(ctx context.Context, keyID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), keyID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return port.ErrNotFound
	}
	return nil
}

// --- User roles ---

func (s *SQLiteStore) UpsertUserRole(ctx context.Context, ur *domain.UserRole) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO user_roles (user_id, namespace, role_mask) VALUES (?, ?, ?)`,
		ur.UserID, ur.Namespace, int(ur.RoleMask))
	return err
}

func (s *SQLiteStore) GetUserRole(ctx context.Context, userID, namespace string) (*domain.UserRole, error) {
	ur := &domain.UserRole{}
	var mask int
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, namespace, role_mask FROM user_roles WHERE user_id = ? AND namespace = ?`,
		userID, namespace).Scan(&ur.UserID, &ur.Namespace, &mask)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ur.RoleMask = domain.Role(mask)
	return ur, nil
}

func (s *SQLiteStore) ListUserRoles(ctx context.Context, userID string) ([]*domain.UserRole, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, namespace, role_mask FROM user_roles WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.UserRole
	for rows.Next() {
		ur := &domain.UserRole{}
		var mask int
		if err := rows.Scan(&ur.UserID, &ur.Namespace, &mask); err != nil {
			return nil, err
		}
		ur.RoleMask = domain.Role(mask)
		out = append(out, ur)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteUserRole(ctx context.Context, userID, namespace string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM user_roles WHERE user_id = ? AND namespace = ?`, userID, namespace)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return port.ErrNotFound
	}
	return nil
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
