package domain

import "time"

// Role bits for bitwise permission checks.
const (
	RoleViewer Role = 1 << 0 // read published versions and bundles
	RoleEditor Role = 1 << 1 // create, edit, and publish rulesets
	RoleAdmin  Role = 1 << 2 // manage users, namespaces, and tokens
)

type Role int

func (r Role) Has(mask Role) bool { return r&mask != 0 }

type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}

type OTPCode struct {
	ID        string
	UserID    string
	CodeHash  string // SHA-256 hex of the raw code
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// APIToken is a long-lived opaque token issued to CLI/CI/SDK consumers.
type APIToken struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Name      string     `json:"name"`
	TokenHash string     `json:"-"` // SHA-256 hex; never returned in responses
	Namespace string     `json:"namespace"`
	Role      Role       `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// UserRole binds a user to a role bitmask within a namespace.
// namespace="*" means the role applies globally (used for admins).
type UserRole struct {
	UserID    string `json:"user_id"`
	Namespace string `json:"namespace"`
	RoleMask  Role   `json:"role_mask"`
}
