package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
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
);
CREATE TABLE IF NOT EXISTS users (
    id            TEXT        NOT NULL PRIMARY KEY,
    email         TEXT        NOT NULL UNIQUE,
    created_at    TIMESTAMPTZ NOT NULL,
    last_login_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS otp_codes (
    id         TEXT        NOT NULL PRIMARY KEY,
    user_id    TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT        NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_otp_codes_user_id ON otp_codes(user_id);
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         TEXT        NOT NULL PRIMARY KEY,
    user_id    TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);
CREATE TABLE IF NOT EXISTS api_keys (
    id         TEXT        NOT NULL PRIMARY KEY,
    name       TEXT        NOT NULL,
    key_hash   TEXT        NOT NULL UNIQUE,
    namespace  TEXT        NOT NULL,
    role       INTEGER     NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);
CREATE TABLE IF NOT EXISTS user_roles (
    user_id   TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    namespace TEXT    NOT NULL,
    role_mask INTEGER NOT NULL,
    PRIMARY KEY (user_id, namespace)
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

func (s *PostgresStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *PostgresStore) Close() error { return s.db.Close() }

func (s *PostgresStore) ListRulesets(ctx context.Context, namespace string, limit, offset int) ([]*domain.Ruleset, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT namespace, key, name, description, created_at, updated_at
         FROM rulesets WHERE namespace = $1 ORDER BY key LIMIT $2 OFFSET $3`, namespace, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Ruleset
	for rows.Next() {
		r := &domain.Ruleset{}
		if err := rows.Scan(&r.Namespace, &r.Key, &r.Name, &r.Description, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.CreatedAt = r.CreatedAt.UTC()
		r.UpdatedAt = r.UpdatedAt.UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateRuleset(ctx context.Context, r *domain.Ruleset) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rulesets (namespace, key, name, description, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6)`,
		r.Namespace, r.Key, r.Name, r.Description,
		r.CreatedAt.UTC(), r.UpdatedAt.UTC(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return port.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *PostgresStore) GetRuleset(ctx context.Context, namespace, key string) (*domain.Ruleset, error) {
	r := &domain.Ruleset{}
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, key, name, description, created_at, updated_at
         FROM rulesets WHERE namespace = $1 AND key = $2`, namespace, key).
		Scan(&r.Namespace, &r.Key, &r.Name, &r.Description, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return r, nil
}

func (s *PostgresStore) GetDraft(ctx context.Context, namespace, key string) (*domain.Draft, error) {
	d := &domain.Draft{}
	var ua time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, dsl, updated_at
         FROM drafts WHERE namespace = $1 AND ruleset_key = $2`, namespace, key).
		Scan(&d.Namespace, &d.RulesetKey, &d.DSL, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.UpdatedAt = ua.UTC()
	return d, nil
}

func (s *PostgresStore) UpsertDraft(ctx context.Context, d *domain.Draft) error {
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
		return port.ErrNotFound
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
		return port.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListVersions(ctx context.Context, namespace, key string, limit, offset int) ([]*domain.Version, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = $1 AND ruleset_key = $2 ORDER BY version ASC LIMIT $3 OFFSET $4`,
		namespace, key, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Version
	for rows.Next() {
		v := &domain.Version{}
		var ca time.Time
		if err := rows.Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca); err != nil {
			return nil, err
		}
		v.CreatedAt = ca.UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetVersion(ctx context.Context, namespace, key string, version int) (*domain.Version, error) {
	v := &domain.Version{}
	var ca time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = $1 AND ruleset_key = $2 AND version = $3`,
		namespace, key, version).
		Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.CreatedAt = ca.UTC()
	return v, nil
}

func (s *PostgresStore) GetLatestVersion(ctx context.Context, namespace, key string) (*domain.Version, error) {
	v := &domain.Version{}
	var ca time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT namespace, ruleset_key, version, checksum, created_at
         FROM versions WHERE namespace = $1 AND ruleset_key = $2
         ORDER BY version DESC LIMIT 1`, namespace, key).
		Scan(&v.Namespace, &v.RulesetKey, &v.Version, &v.Checksum, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.CreatedAt = ca.UTC()
	return v, nil
}

func (s *PostgresStore) CreateVersion(ctx context.Context, v *domain.Version) error {
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
		return port.ErrVersionImmutable
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

// --- User ---

func (s *PostgresStore) CreateUser(ctx context.Context, u *domain.User) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, email, created_at, last_login_at) VALUES ($1, $2, $3, $4)`,
		u.ID, u.Email, u.CreatedAt.UTC(), u.LastLoginAt.UTC())
	if err != nil {
		if isUniqueViolation(err) {
			return port.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	u := &domain.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, created_at, last_login_at FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = u.CreatedAt.UTC()
	u.LastLoginAt = u.LastLoginAt.UTC()
	return u, nil
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	u := &domain.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, created_at, last_login_at FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = u.CreatedAt.UTC()
	u.LastLoginAt = u.LastLoginAt.UTC()
	return u, nil
}

func (s *PostgresStore) UpdateUserLastLogin(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = $1 WHERE id = $2`, time.Now().UTC(), userID)
	return err
}

func (s *PostgresStore) ListUsers(ctx context.Context, limit, offset int) ([]*domain.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, created_at, last_login_at FROM users ORDER BY email LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.User
	for rows.Next() {
		u := &domain.User{}
		if err := rows.Scan(&u.ID, &u.Email, &u.CreatedAt, &u.LastLoginAt); err != nil {
			return nil, err
		}
		u.CreatedAt = u.CreatedAt.UTC()
		u.LastLoginAt = u.LastLoginAt.UTC()
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *PostgresStore) DeleteUser(ctx context.Context, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
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

func (s *PostgresStore) CreateOTPCode(ctx context.Context, otp *domain.OTPCode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO otp_codes (id, user_id, code_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		otp.ID, otp.UserID, otp.CodeHash, otp.ExpiresAt.UTC())
	return err
}

func (s *PostgresStore) GetUnusedOTPCode(ctx context.Context, userID string) (*domain.OTPCode, error) {
	otp := &domain.OTPCode{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, code_hash, expires_at FROM otp_codes
         WHERE user_id = $1 AND used_at IS NULL AND expires_at > $2
         ORDER BY expires_at DESC LIMIT 1`,
		userID, time.Now().UTC()).
		Scan(&otp.ID, &otp.UserID, &otp.CodeHash, &otp.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	otp.ExpiresAt = otp.ExpiresAt.UTC()
	return otp, nil
}

func (s *PostgresStore) MarkOTPUsed(ctx context.Context, otpID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE otp_codes SET used_at = $1 WHERE id = $2`, time.Now().UTC(), otpID)
	return err
}

func (s *PostgresStore) DeleteExpiredOTPs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM otp_codes WHERE expires_at < $1`, time.Now().UTC())
	return err
}

// --- Refresh token ---

func (s *PostgresStore) CreateRefreshToken(ctx context.Context, t *domain.RefreshToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		t.ID, t.UserID, t.TokenHash, t.ExpiresAt.UTC())
	return err
}

func (s *PostgresStore) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	t := &domain.RefreshToken{}
	var exp time.Time
	var rev sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash).Scan(&t.ID, &t.UserID, &t.TokenHash, &exp, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.ExpiresAt = exp.UTC()
	if rev.Valid {
		tt := rev.Time.UTC()
		t.RevokedAt = &tt
	}
	return t, nil
}

func (s *PostgresStore) RevokeRefreshToken(ctx context.Context, tokenID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL`,
		time.Now().UTC(), tokenID)
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

func (s *PostgresStore) CreateAPIKey(ctx context.Context, k *domain.APIKey) error {
	var exp interface{}
	if k.ExpiresAt != nil {
		exp = k.ExpiresAt.UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, name, key_hash, namespace, role, created_at, expires_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		k.ID, k.Name, k.KeyHash, k.Namespace, int(k.Role), k.CreatedAt.UTC(), exp)
	if err != nil {
		if isUniqueViolation(err) {
			return port.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *PostgresStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (*domain.APIKey, error) {
	k := &domain.APIKey{}
	var role int
	var exp, rev sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, key_hash, namespace, role, created_at, expires_at, revoked_at
         FROM api_keys WHERE key_hash = $1`, keyHash).
		Scan(&k.ID, &k.Name, &k.KeyHash, &k.Namespace, &role, &k.CreatedAt, &exp, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	k.Role = domain.Role(role)
	k.CreatedAt = k.CreatedAt.UTC()
	if exp.Valid {
		tt := exp.Time.UTC()
		k.ExpiresAt = &tt
	}
	if rev.Valid {
		tt := rev.Time.UTC()
		k.RevokedAt = &tt
	}
	return k, nil
}

func (s *PostgresStore) ListAPIKeys(ctx context.Context, limit, offset int) ([]*domain.APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, key_hash, namespace, role, created_at, expires_at, revoked_at
         FROM api_keys ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.APIKey
	for rows.Next() {
		k := &domain.APIKey{}
		var role int
		var exp, rev sql.NullTime
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyHash, &k.Namespace, &role, &k.CreatedAt, &exp, &rev); err != nil {
			return nil, err
		}
		k.Role = domain.Role(role)
		k.CreatedAt = k.CreatedAt.UTC()
		if exp.Valid {
			tt := exp.Time.UTC()
			k.ExpiresAt = &tt
		}
		if rev.Valid {
			tt := rev.Time.UTC()
			k.RevokedAt = &tt
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *PostgresStore) RevokeAPIKey(ctx context.Context, keyID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL`,
		time.Now().UTC(), keyID)
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

func (s *PostgresStore) UpsertUserRole(ctx context.Context, ur *domain.UserRole) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_roles (user_id, namespace, role_mask) VALUES ($1, $2, $3)
         ON CONFLICT (user_id, namespace) DO UPDATE SET role_mask = EXCLUDED.role_mask`,
		ur.UserID, ur.Namespace, int(ur.RoleMask))
	return err
}

func (s *PostgresStore) GetUserRole(ctx context.Context, userID, namespace string) (*domain.UserRole, error) {
	ur := &domain.UserRole{}
	var mask int
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, namespace, role_mask FROM user_roles WHERE user_id = $1 AND namespace = $2`,
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

func (s *PostgresStore) ListUserRoles(ctx context.Context, userID string) ([]*domain.UserRole, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, namespace, role_mask FROM user_roles WHERE user_id = $1`, userID)
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

func (s *PostgresStore) DeleteUserRole(ctx context.Context, userID, namespace string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM user_roles WHERE user_id = $1 AND namespace = $2`, userID, namespace)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return port.ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "23505") ||
		strings.Contains(err.Error(), "unique constraint") ||
		strings.Contains(err.Error(), "duplicate key")
}
