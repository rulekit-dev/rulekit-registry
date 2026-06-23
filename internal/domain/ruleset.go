package domain

import (
	"encoding/json"
	"time"
)

type Workspace struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type Ruleset struct {
	Workspace   string    `json:"workspace"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Draft struct {
	Workspace  string          `json:"workspace"`
	RulesetKey string          `json:"ruleset_key"`
	DSL        json.RawMessage `json:"dsl"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type Version struct {
	Workspace  string          `json:"workspace"`
	RulesetKey string          `json:"ruleset_key"`
	Version    int             `json:"version"`
	Checksum   string          `json:"checksum"` // "sha256:<hex>"
	DSL        json.RawMessage `json:"dsl"`
	CreatedAt  time.Time       `json:"created_at"`
}

type VersionManifest struct {
	Workspace  string    `json:"workspace"`
	RulesetKey string    `json:"ruleset_key"`
	Version    int       `json:"version"`
	Checksum   string    `json:"checksum"`
	CreatedAt  time.Time `json:"created_at"`
}

type TraceEntry struct {
	RuleID   string `json:"rule_id"`
	RuleName string `json:"rule_name"`
	NodeID   string `json:"node_id"`
	Matched  bool   `json:"matched"`
}

type EvalResult struct {
	Result map[string]any `json:"result"`
	Trace  []TraceEntry   `json:"trace"`
}
