package domain

import "time"

const (
	RoleViewer Role = 1 << 0                    // read published versions and bundles
	RoleEditor Role = RoleViewer | 1<<1         // create, edit, and publish rulesets (implies viewer)
	RoleAdmin  Role = RoleEditor | 1<<2         // manage users, namespaces, and keys (implies editor+viewer)
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

// RefreshToken is a short-lived token issued at login and rotated on each refresh.
type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string // SHA-256 hex; never returned to callers
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// APIKey is a long-lived credential for machine/CLI access, not tied to a user.
type APIKey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	KeyHash   string     `json:"-"` // SHA-256 hex; never returned in responses
	Namespace string     `json:"namespace"`
	Role      Role       `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type UserRole struct {
	UserID    string `json:"user_id"`
	Namespace string `json:"namespace"`
	RoleMask  Role   `json:"role_mask"`
}
