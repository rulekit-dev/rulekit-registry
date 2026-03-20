package domain

import (
	"encoding/json"
	"time"
)

type Ruleset struct {
	Namespace   string    `json:"namespace"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Draft struct {
	Namespace  string          `json:"namespace"`
	RulesetKey string          `json:"ruleset_key"`
	DSL        json.RawMessage `json:"dsl"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type Version struct {
	Namespace  string          `json:"namespace"`
	RulesetKey string          `json:"ruleset_key"`
	Version    int             `json:"version"`
	Checksum   string          `json:"checksum"` // "sha256:<hex>"
	DSL        json.RawMessage `json:"dsl"`
	CreatedAt  time.Time       `json:"created_at"`
}

type VersionManifest struct {
	Namespace  string    `json:"namespace"`
	RulesetKey string    `json:"ruleset_key"`
	Version    int       `json:"version"`
	Checksum   string    `json:"checksum"`
	CreatedAt  time.Time `json:"created_at"`
}
