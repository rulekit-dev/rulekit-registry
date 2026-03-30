package dsl_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/domain/dsl"
)

func validDSLBytes() []byte {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"age":    map[string]any{"type": "number", "direction": "input"},
			"name":   map[string]any{"type": "string", "direction": "input"},
			"active": map[string]any{"type": "boolean", "direction": "input"},
			"status": map[string]any{
				"type":      "enum",
				"direction": "input",
				"options":   []string{"pending", "active", "closed"},
			},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id":       "node-1",
				"strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id":   "rule-1",
						"name": "Adult active user",
						"when": []any{
							map[string]any{"field": "age", "op": "gte", "value": 18},
							map[string]any{"field": "active", "op": "eq", "value": true},
							map[string]any{"field": "name", "op": "starts_with", "value": "A"},
							map[string]any{"field": "status", "op": "eq", "value": "active"},
						},
						"then": map[string]any{"result": "approved"},
					},
				},
			},
		},
	}
	b, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}
	return b
}

func TestValidDSL(t *testing.T) {
	_, err := dsl.ParseAndValidate(validDSLBytes())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestMultiNodeWithEdge(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"age":      map[string]any{"type": "number", "direction": "input"},
			"score":    map[string]any{"type": "number", "direction": "input"},
			"eligible": map[string]any{"type": "boolean", "direction": "output"},
			"tier":     map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "eligibility",
		"nodes": []any{
			map[string]any{
				"id":       "eligibility",
				"strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "adult",
						"when": []any{map[string]any{"field": "age", "op": "gte", "value": 18}},
						"then": map[string]any{"eligible": true},
					},
				},
			},
			map[string]any{
				"id":       "pricing",
				"strategy": "all_matches",
				"rules": []any{
					map[string]any{
						"id": "r2", "name": "high-score",
						"when": []any{map[string]any{"field": "score", "op": "gt", "value": 90}},
						"then": map[string]any{"tier": "gold"},
					},
				},
			},
		},
		"edges": []any{
			map[string]any{"from": "eligibility", "to": "pricing", "map": map[string]any{"score": "credit_score"}},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err != nil {
		t.Fatalf("expected no error for multi-node DSL, got: %v", err)
	}
}

func TestUnknownDSLVersion(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v2",
		"schema":      map[string]any{"x": map[string]any{"type": "number", "direction": "input"}},
		"entry":       "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "x", "op": "eq", "value": 1}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for unknown dsl_version, got nil")
	}
	if !strings.Contains(err.Error(), "v2") {
		t.Errorf("error should mention the invalid version, got: %v", err)
	}
}

func TestMissingEntry(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"x":      map[string]any{"type": "number", "direction": "input"},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "does-not-exist",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "x", "op": "eq", "value": 1}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for invalid entry node, got nil")
	}
	if !strings.Contains(err.Error(), "entry") {
		t.Errorf("error should mention entry, got: %v", err)
	}
}

func TestDuplicateNodeID(t *testing.T) {
	node := map[string]any{
		"id": "node-1", "strategy": "first_match",
		"rules": []any{
			map[string]any{
				"id": "r1", "name": "rule",
				"when": []any{map[string]any{"field": "x", "op": "eq", "value": 1}},
				"then": map[string]any{"result": "ok"},
			},
		},
	}
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"x":      map[string]any{"type": "number", "direction": "input"},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{node, node},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for duplicate node id, got nil")
	}
}

func TestEdgeInvalidFrom(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"x":      map[string]any{"type": "number", "direction": "input"},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "x", "op": "eq", "value": 1}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
		"edges": []any{
			map[string]any{"from": "nonexistent", "to": "node-1"},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for invalid edge from, got nil")
	}
}

func TestEdgeSelfLoop(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"x":      map[string]any{"type": "number", "direction": "input"},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "x", "op": "eq", "value": 1}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
		"edges": []any{
			map[string]any{"from": "node-1", "to": "node-1"},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for self-loop edge, got nil")
	}
}

func TestUnknownFieldReference(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"age":    map[string]any{"type": "number", "direction": "input"},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "nonexistent_field", "op": "eq", "value": 1}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for unknown field reference, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent_field") {
		t.Errorf("error should contain the field name, got: %v", err)
	}
}

func TestInvalidOperatorForType(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"active": map[string]any{"type": "boolean", "direction": "input"},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "active", "op": "gt", "value": true}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for invalid operator on boolean field, got nil")
	}
}

func TestValueTypeMismatch(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"age":    map[string]any{"type": "number", "direction": "input"},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "age", "op": "eq", "value": "not-a-number"}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for value type mismatch, got nil")
	}
}

func TestEnumInvalidOption(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"status": map[string]any{"type": "enum", "direction": "input", "options": []string{"pending", "active"}},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "status", "op": "eq", "value": "unknown_option"}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for invalid enum option, got nil")
	}
}

func TestEnumInOperator(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"status": map[string]any{"type": "enum", "direction": "input", "options": []string{"pending", "active", "closed"}},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "status", "op": "in", "value": []string{"pending", "active"}}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err != nil {
		t.Fatalf("expected no error for valid enum 'in' operator, got: %v", err)
	}
}

func TestDeterministicSerialization(t *testing.T) {
	input := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"z_field": map[string]any{"type": "number", "direction": "input"},
			"a_field": map[string]any{"type": "string", "direction": "input"},
			"m_field": map[string]any{"type": "boolean", "direction": "input"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "z_field", "op": "gt", "value": 0}},
						"then": map[string]any{"z": 1, "a": 2, "m": 3},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(input)

	out1, err := dsl.MarshalDeterministic(json.RawMessage(b))
	if err != nil {
		t.Fatalf("first MarshalDeterministic failed: %v", err)
	}
	out2, err := dsl.MarshalDeterministic(json.RawMessage(b))
	if err != nil {
		t.Fatalf("second MarshalDeterministic failed: %v", err)
	}
	if string(out1) != string(out2) {
		t.Errorf("MarshalDeterministic is not deterministic:\nout1: %s\nout2: %s", out1, out2)
	}
}

func TestChecksumFormat(t *testing.T) {
	data := []byte(`{"dsl_version":"v1"}`)
	checksum := dsl.Checksum(data)
	if !strings.HasPrefix(checksum, "sha256:") {
		t.Errorf("expected checksum to start with 'sha256:', got: %q", checksum)
	}
	const expectedLen = 7 + 64
	if len(checksum) != expectedLen {
		t.Errorf("expected checksum length %d, got %d: %q", expectedLen, len(checksum), checksum)
	}
}

func TestEmptySchema(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema":      map[string]any{},
		"entry":       "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for empty schema, got nil")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Errorf("error should mention schema, got: %v", err)
	}
}

func TestConditionOnOutputFieldRejected(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "result", "op": "eq", "value": "ok"}},
						"then": map[string]any{"result": "ok"},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for condition on output field, got nil")
	}
	if !strings.Contains(err.Error(), "not an input field") {
		t.Errorf("error should mention input field, got: %v", err)
	}
}

func TestThenKeyOnInputFieldRejected(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"age":    map[string]any{"type": "number", "direction": "input"},
			"result": map[string]any{"type": "string", "direction": "output"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{
					map[string]any{
						"id": "r1", "name": "rule",
						"when": []any{map[string]any{"field": "age", "op": "eq", "value": 18}},
						"then": map[string]any{"age": 20},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for then key on input field, got nil")
	}
	if !strings.Contains(err.Error(), "not an output field") {
		t.Errorf("error should mention output field, got: %v", err)
	}
}

func TestMissingDirectionRejected(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"schema": map[string]any{
			"x": map[string]any{"type": "number"},
		},
		"entry": "node-1",
		"nodes": []any{
			map[string]any{
				"id": "node-1", "strategy": "first_match",
				"rules": []any{},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for missing direction, got nil")
	}
	if !strings.Contains(err.Error(), "direction") {
		t.Errorf("error should mention direction, got: %v", err)
	}
}
