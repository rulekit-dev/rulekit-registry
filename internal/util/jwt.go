package util

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
)

const (
	AccessTokenTTL  = 15 * time.Minute
	AdminTokenTTL   = 24 * time.Hour
	RefreshTokenTTL = 7 * 24 * time.Hour
)

type Claims struct {
	jwt.RegisteredClaims
	Email string      `json:"email"`
	Roles []RoleClaim `json:"roles"`
}

type RoleClaim struct {
	Namespace string      `json:"ns"`
	RoleMask  domain.Role `json:"mask"`
}

func SignAccessToken(secret []byte, user *domain.User, roles []*domain.UserRole) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Email: user.Email,
		Roles: toRoleClaims(roles),
	}
	return signClaims(secret, claims)
}

// SignAdminToken issues a long-lived token for the virtual admin identity.
func SignAdminToken(secret []byte) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "admin",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AdminTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Email: "admin",
		Roles: []RoleClaim{{Namespace: "*", RoleMask: domain.RoleAdmin}},
	}
	return signClaims(secret, claims)
}

func ParseAccessToken(secret []byte, tokenStr string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	claims, ok := t.Claims.(*Claims)
	if !ok || !t.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
)

// RoleForNamespace returns the effective role mask for a namespace.
// A global role (namespace="*") supersedes namespace-specific roles.
func (c *Claims) RoleForNamespace(namespace string) domain.Role {
	var mask domain.Role
	for _, r := range c.Roles {
		if r.Namespace == "*" {
			return r.RoleMask
		}
		if r.Namespace == namespace {
			mask = r.RoleMask
		}
	}
	return mask
}

func toRoleClaims(roles []*domain.UserRole) []RoleClaim {
	out := make([]RoleClaim, len(roles))
	for i, r := range roles {
		out[i] = RoleClaim{Namespace: r.Namespace, RoleMask: r.RoleMask}
	}
	return out
}

func signClaims(secret []byte, claims Claims) (string, error) {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token, err := t.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("jwtutil: sign: %w", err)
	}
	return token, nil
}
