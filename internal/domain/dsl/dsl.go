package dsl

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

type FieldType string

const (
	FieldTypeNumber  FieldType = "number"
	FieldTypeString  FieldType = "string"
	FieldTypeBoolean FieldType = "boolean"
	FieldTypeEnum    FieldType = "enum"
)

type Strategy string

const (
	StrategyFirstMatch Strategy = "first_match"
	StrategyAllMatches Strategy = "all_matches"
)

type FieldDef struct {
	Type    FieldType `json:"type"`
	Options []string  `json:"options,omitempty"` // required for enum
}

type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

type Rule struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	When []Condition    `json:"when"`
	Then map[string]any `json:"then"`
}

type DSL struct {
	DSLVersion string              `json:"dsl_version"`
	Strategy   Strategy            `json:"strategy"`
	Schema     map[string]FieldDef `json:"schema"`
	Rules      []Rule              `json:"rules"`
	Default    map[string]any      `json:"default,omitempty"`
}

var validOps = map[FieldType]map[string]bool{
	FieldTypeNumber: {
		"eq": true, "ne": true, "gt": true, "gte": true, "lt": true, "lte": true,
	},
	FieldTypeString: {
		"eq": true, "ne": true, "contains": true, "starts_with": true, "ends_with": true,
	},
	FieldTypeBoolean: {
		"eq": true, "ne": true,
	},
	FieldTypeEnum: {
		"eq": true, "ne": true, "in": true,
	},
}

func Parse(data []byte) (*DSL, error) {
	var d DSL
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("dsl: parse error: %w", err)
	}
	return &d, nil
}

func Validate(d *DSL) error {
	if d.DSLVersion != "v1" {
		return fmt.Errorf("dsl: unsupported dsl_version %q, expected \"v1\"", d.DSLVersion)
	}
	if d.Strategy != StrategyFirstMatch && d.Strategy != StrategyAllMatches {
		return fmt.Errorf("dsl: unknown strategy %q", d.Strategy)
	}
	if len(d.Schema) == 0 {
		return fmt.Errorf("dsl: schema must not be empty")
	}

	for fieldName, fd := range d.Schema {
		if err := validateFieldDef(fieldName, fd); err != nil {
			return err
		}
	}

	seenIDs := make(map[string]bool, len(d.Rules))
	for i, rule := range d.Rules {
		if rule.ID == "" {
			return fmt.Errorf("dsl: rule[%d] missing id", i)
		}
		if seenIDs[rule.ID] {
			return fmt.Errorf("dsl: duplicate rule id %q", rule.ID)
		}
		seenIDs[rule.ID] = true

		if len(rule.When) == 0 {
			return fmt.Errorf("dsl: rule %q has no conditions", rule.ID)
		}
		for j, cond := range rule.When {
			if err := validateCondition(fmt.Sprintf("rule %q condition[%d]", rule.ID, j), cond, d.Schema); err != nil {
				return err
			}
		}
		if len(rule.Then) == 0 {
			return fmt.Errorf("dsl: rule %q has empty then clause", rule.ID)
		}
	}

	return nil
}

func ParseAndValidate(data []byte) (*DSL, error) {
	d, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := Validate(d); err != nil {
		return nil, err
	}
	return d, nil
}

func validateFieldDef(name string, fd FieldDef) error {
	switch fd.Type {
	case FieldTypeNumber, FieldTypeString, FieldTypeBoolean:
		// valid
	case FieldTypeEnum:
		if len(fd.Options) == 0 {
			return fmt.Errorf("dsl: field %q is type enum but has no options", name)
		}
	default:
		return fmt.Errorf("dsl: field %q has unknown type %q", name, fd.Type)
	}
	return nil
}

func validateCondition(loc string, cond Condition, schema map[string]FieldDef) error {
	if cond.Field == "" {
		return fmt.Errorf("dsl: %s missing field", loc)
	}
	fd, ok := schema[cond.Field]
	if !ok {
		return fmt.Errorf("dsl: %s references unknown field %q", loc, cond.Field)
	}
	ops, ok := validOps[fd.Type]
	if !ok {
		return fmt.Errorf("dsl: %s field %q has unknown type %q", loc, cond.Field, fd.Type)
	}
	if !ops[cond.Op] {
		return fmt.Errorf("dsl: %s operator %q is not valid for field type %q (allowed: %s)",
			loc, cond.Op, fd.Type, joinKeys(ops))
	}
	return validateConditionValue(loc, cond, fd)
}

func validateConditionValue(loc string, cond Condition, fd FieldDef) error {
	if cond.Value == nil {
		return fmt.Errorf("dsl: %s value is null", loc)
	}

	switch fd.Type {
	case FieldTypeNumber:
		switch cond.Value.(type) {
		case float64, json.Number:
			// ok
		default:
			return fmt.Errorf("dsl: %s value must be a number, got %T", loc, cond.Value)
		}

	case FieldTypeBoolean:
		if _, ok := cond.Value.(bool); !ok {
			return fmt.Errorf("dsl: %s value must be a boolean, got %T", loc, cond.Value)
		}

	case FieldTypeString:
		if _, ok := cond.Value.(string); !ok {
			return fmt.Errorf("dsl: %s value must be a string, got %T", loc, cond.Value)
		}

	case FieldTypeEnum:
		if cond.Op == "in" {
			arr, ok := cond.Value.([]any)
			if !ok {
				return fmt.Errorf("dsl: %s operator \"in\" requires an array value", loc)
			}
			for k, v := range arr {
				s, ok := v.(string)
				if !ok {
					return fmt.Errorf("dsl: %s operator \"in\" array[%d] must be a string", loc, k)
				}
				if !containsOption(fd.Options, s) {
					return fmt.Errorf("dsl: %s operator \"in\" value %q is not a valid enum option", loc, s)
				}
			}
		} else {
			s, ok := cond.Value.(string)
			if !ok {
				return fmt.Errorf("dsl: %s enum value must be a string, got %T", loc, cond.Value)
			}
			if !containsOption(fd.Options, s) {
				return fmt.Errorf("dsl: %s value %q is not a valid enum option (allowed: %s)",
					loc, s, strings.Join(fd.Options, ", "))
			}
		}
	}
	return nil
}

func MarshalDeterministic(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

// Checksum returns the SHA-256 hash of the data in "sha256:<hex>" format.
func Checksum(deterministicDSL []byte) string {
	sum := sha256.Sum256(deterministicDSL)
	return fmt.Sprintf("sha256:%x", sum)
}

func joinKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}

func containsOption(options []string, s string) bool {
	for _, o := range options {
		if o == s {
			return true
		}
	}
	return false
}
