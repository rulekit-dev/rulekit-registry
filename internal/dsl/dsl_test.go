package dsl_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rulekit-dev/rulekit-registry/internal/dsl"
)

// validDSLBytes returns a complete, valid DSL JSON with number, string, boolean,
// and enum fields covering multiple rule conditions.
func validDSLBytes() []byte {
	d := map[string]any{
		"dsl_version": "v1",
		"strategy":    "first_match",
		"schema": map[string]any{
			"age": map[string]any{
				"type": "number",
			},
			"name": map[string]any{
				"type": "string",
			},
			"active": map[string]any{
				"type": "boolean",
			},
			"status": map[string]any{
				"type":    "enum",
				"options": []string{"pending", "active", "closed"},
			},
		},
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
	}
	b, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}
	return b
}

// TestValidDSL verifies that a well-formed DSL passes ParseAndValidate without error.
func TestValidDSL(t *testing.T) {
	_, err := dsl.ParseAndValidate(validDSLBytes())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// TestUnknownDSLVersion verifies that a dsl_version other than "v1" returns an error.
func TestUnknownDSLVersion(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v2",
		"strategy":    "first_match",
		"schema": map[string]any{
			"x": map[string]any{"type": "number"},
		},
		"rules": []any{
			map[string]any{
				"id":   "r1",
				"name": "rule",
				"when": []any{
					map[string]any{"field": "x", "op": "eq", "value": 1},
				},
				"then": map[string]any{"result": "ok"},
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

// TestUnknownFieldReference verifies that a condition referencing a field not in
// the schema returns an error containing the field name.
func TestUnknownFieldReference(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"strategy":    "first_match",
		"schema": map[string]any{
			"age": map[string]any{"type": "number"},
		},
		"rules": []any{
			map[string]any{
				"id":   "r1",
				"name": "rule",
				"when": []any{
					map[string]any{"field": "nonexistent_field", "op": "eq", "value": 1},
				},
				"then": map[string]any{"result": "ok"},
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

// TestInvalidOperatorForType verifies that using "gt" on a boolean field returns an error.
func TestInvalidOperatorForType(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"strategy":    "first_match",
		"schema": map[string]any{
			"active": map[string]any{"type": "boolean"},
		},
		"rules": []any{
			map[string]any{
				"id":   "r1",
				"name": "rule",
				"when": []any{
					map[string]any{"field": "active", "op": "gt", "value": true},
				},
				"then": map[string]any{"result": "ok"},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for invalid operator on boolean field, got nil")
	}
}

// TestValueTypeMismatch verifies that a number field with a string value returns an error.
func TestValueTypeMismatch(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"strategy":    "first_match",
		"schema": map[string]any{
			"age": map[string]any{"type": "number"},
		},
		"rules": []any{
			map[string]any{
				"id":   "r1",
				"name": "rule",
				"when": []any{
					map[string]any{"field": "age", "op": "eq", "value": "not-a-number"},
				},
				"then": map[string]any{"result": "ok"},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for value type mismatch, got nil")
	}
}

// TestEnumInvalidOption verifies that an enum field with a value not in options returns an error.
func TestEnumInvalidOption(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"strategy":    "first_match",
		"schema": map[string]any{
			"status": map[string]any{
				"type":    "enum",
				"options": []string{"pending", "active"},
			},
		},
		"rules": []any{
			map[string]any{
				"id":   "r1",
				"name": "rule",
				"when": []any{
					map[string]any{"field": "status", "op": "eq", "value": "unknown_option"},
				},
				"then": map[string]any{"result": "ok"},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err == nil {
		t.Fatal("expected error for invalid enum option, got nil")
	}
}

// TestEnumInOperator verifies that the "in" operator with a valid array of enum options passes.
func TestEnumInOperator(t *testing.T) {
	d := map[string]any{
		"dsl_version": "v1",
		"strategy":    "first_match",
		"schema": map[string]any{
			"status": map[string]any{
				"type":    "enum",
				"options": []string{"pending", "active", "closed"},
			},
		},
		"rules": []any{
			map[string]any{
				"id":   "r1",
				"name": "rule",
				"when": []any{
					map[string]any{
						"field": "status",
						"op":    "in",
						"value": []string{"pending", "active"},
					},
				},
				"then": map[string]any{"result": "ok"},
			},
		},
	}
	b, _ := json.Marshal(d)
	_, err := dsl.ParseAndValidate(b)
	if err != nil {
		t.Fatalf("expected no error for valid enum 'in' operator, got: %v", err)
	}
}

// TestDeterministicSerialization verifies that marshaling the same struct twice
// (even with maps whose key iteration order may differ) yields byte-identical output.
func TestDeterministicSerialization(t *testing.T) {
	// Build two maps with the same content; map iteration order in Go is randomised.
	input := map[string]any{
		"dsl_version": "v1",
		"strategy":    "first_match",
		"schema": map[string]any{
			"z_field": map[string]any{"type": "number"},
			"a_field": map[string]any{"type": "string"},
			"m_field": map[string]any{"type": "boolean"},
		},
		"rules": []any{
			map[string]any{
				"id":   "r1",
				"name": "rule",
				"when": []any{
					map[string]any{"field": "z_field", "op": "gt", "value": 0},
				},
				"then": map[string]any{"z": 1, "a": 2, "m": 3},
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

// TestChecksumFormat verifies that Checksum returns a string starting with "sha256:".
func TestChecksumFormat(t *testing.T) {
	data := []byte(`{"dsl_version":"v1"}`)
	checksum := dsl.Checksum(data)
	if !strings.HasPrefix(checksum, "sha256:") {
		t.Errorf("expected checksum to start with 'sha256:', got: %q", checksum)
	}
	// sha256 hex is 64 characters; "sha256:" is 7 chars, total 71.
	const expectedLen = 7 + 64
	if len(checksum) != expectedLen {
		t.Errorf("expected checksum length %d, got %d: %q", expectedLen, len(checksum), checksum)
	}
}
