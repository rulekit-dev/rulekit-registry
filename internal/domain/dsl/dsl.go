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

type FieldDirection string

const (
	FieldDirectionInput  FieldDirection = "input"
	FieldDirectionOutput FieldDirection = "output"
)

type FieldDef struct {
	Type      FieldType      `json:"type"`
	Direction FieldDirection `json:"direction"`
	Options   []string       `json:"options,omitempty"` // required for enum
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

// RuleNode is an evaluation unit on the canvas.
// Its rule conditions reference fields from the top-level Schema.
type RuleNode struct {
	ID       string         `json:"id"`
	Name     string         `json:"name,omitempty"`
	Strategy Strategy       `json:"strategy"`
	Rules    []Rule         `json:"rules"`
	Default  map[string]any `json:"default,omitempty"`
}

// Edge connects two nodes on the canvas.
// Map optionally describes how output fields from the source node
// become input fields on the destination node (output key -> input key).
type Edge struct {
	From string            `json:"from"`
	To   string            `json:"to"`
	Map  map[string]string `json:"map,omitempty"`
}

type DSL struct {
	DSLVersion string              `json:"dsl_version"`
	Schema     map[string]FieldDef `json:"schema"`
	Entry      string              `json:"entry"`
	Nodes      []RuleNode          `json:"nodes"`
	Edges      []Edge              `json:"edges,omitempty"`
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
	if len(d.Schema) == 0 {
		return fmt.Errorf("dsl: schema must not be empty")
	}
	if d.Entry == "" {
		return fmt.Errorf("dsl: entry must not be empty")
	}
	if len(d.Nodes) == 0 {
		return fmt.Errorf("dsl: nodes must not be empty")
	}

	for fieldName, fd := range d.Schema {
		if err := validateFieldDef("schema", fieldName, fd); err != nil {
			return err
		}
	}

	nodeIDs := make(map[string]bool, len(d.Nodes))
	for i, node := range d.Nodes {
		if node.ID == "" {
			return fmt.Errorf("dsl: nodes[%d] missing id", i)
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("dsl: duplicate node id %q", node.ID)
		}
		nodeIDs[node.ID] = true
	}

	if !nodeIDs[d.Entry] {
		return fmt.Errorf("dsl: entry %q does not reference a valid node id", d.Entry)
	}

	for i, edge := range d.Edges {
		if !nodeIDs[edge.From] {
			return fmt.Errorf("dsl: edges[%d] from %q does not reference a valid node id", i, edge.From)
		}
		if !nodeIDs[edge.To] {
			return fmt.Errorf("dsl: edges[%d] to %q does not reference a valid node id", i, edge.To)
		}
		if edge.From == edge.To {
			return fmt.Errorf("dsl: edges[%d] self-loop on node %q", i, edge.From)
		}
		for _, destField := range edge.Map {
			if _, ok := d.Schema[destField]; !ok {
				return fmt.Errorf("dsl: edges[%d] map destination field %q is not defined in schema", i, destField)
			}
		}
	}

	nodeIncomingFields := buildNodeIncomingFields(d.Edges, d.Schema)
	for _, node := range d.Nodes {
		if err := validateNode(node, d.Schema, nodeIncomingFields[node.ID]); err != nil {
			return err
		}
	}

	return nil
}

// buildNodeIncomingFields returns per-node sets of field names that are
// available as inputs from upstream edges. When an edge has no Map, all
// output fields from the schema are implicitly available to the destination.
func buildNodeIncomingFields(edges []Edge, schema map[string]FieldDef) map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	for _, edge := range edges {
		if result[edge.To] == nil {
			result[edge.To] = make(map[string]bool)
		}
		if len(edge.Map) == 0 {
			for name, fd := range schema {
				if fd.Direction == FieldDirectionOutput {
					result[edge.To][name] = true
				}
			}
		} else {
			for _, destField := range edge.Map {
				result[edge.To][destField] = true
			}
		}
	}
	return result
}

func validateNode(node RuleNode, schema map[string]FieldDef, extraInputs map[string]bool) error {
	if node.Strategy != StrategyFirstMatch && node.Strategy != StrategyAllMatches {
		return fmt.Errorf("dsl: node %q has unknown strategy %q", node.ID, node.Strategy)
	}

	seenIDs := make(map[string]bool, len(node.Rules))
	for i, rule := range node.Rules {
		if rule.ID == "" {
			return fmt.Errorf("dsl: node %q rules[%d] missing id", node.ID, i)
		}
		if seenIDs[rule.ID] {
			return fmt.Errorf("dsl: node %q duplicate rule id %q", node.ID, rule.ID)
		}
		seenIDs[rule.ID] = true

		if len(rule.When) == 0 {
			return fmt.Errorf("dsl: node %q rule %q has no conditions", node.ID, rule.ID)
		}
		for j, cond := range rule.When {
			if err := validateCondition(fmt.Sprintf("node %q rule %q condition[%d]", node.ID, rule.ID, j), cond, schema, extraInputs); err != nil {
				return err
			}
		}
		if len(rule.Then) == 0 {
			return fmt.Errorf("dsl: node %q rule %q has empty then clause", node.ID, rule.ID)
		}
		for key := range rule.Then {
			fd, ok := schema[key]
			if !ok {
				return fmt.Errorf("dsl: node %q rule %q then key %q is not defined in schema", node.ID, rule.ID, key)
			}
			if fd.Direction != FieldDirectionOutput {
				return fmt.Errorf("dsl: node %q rule %q then key %q is not an output field", node.ID, rule.ID, key)
			}
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

// ValidateDraft is a lenient validation for work-in-progress drafts.
// It checks structural integrity (valid types, no duplicates, valid references)
// but allows incomplete data (empty schema, empty nodes, missing conditions).
func ValidateDraft(d *DSL) error {
	if d.DSLVersion != "v1" {
		return fmt.Errorf("dsl: unsupported dsl_version %q, expected \"v1\"", d.DSLVersion)
	}

	for fieldName, fd := range d.Schema {
		if err := validateFieldDef("schema", fieldName, fd); err != nil {
			return err
		}
	}

	nodeIDs := make(map[string]bool, len(d.Nodes))
	for i, node := range d.Nodes {
		if node.ID == "" {
			return fmt.Errorf("dsl: nodes[%d] missing id", i)
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("dsl: duplicate node id %q", node.ID)
		}
		nodeIDs[node.ID] = true

		if node.Strategy != StrategyFirstMatch && node.Strategy != StrategyAllMatches {
			return fmt.Errorf("dsl: node %q has unknown strategy %q", node.ID, node.Strategy)
		}

		seenIDs := make(map[string]bool, len(node.Rules))
		for j, rule := range node.Rules {
			if rule.ID == "" {
				return fmt.Errorf("dsl: node %q rules[%d] missing id", node.ID, j)
			}
			if seenIDs[rule.ID] {
				return fmt.Errorf("dsl: node %q duplicate rule id %q", node.ID, rule.ID)
			}
			seenIDs[rule.ID] = true
		}
	}

	if d.Entry != "" && !nodeIDs[d.Entry] {
		return fmt.Errorf("dsl: entry %q does not reference a valid node id", d.Entry)
	}

	for i, edge := range d.Edges {
		if !nodeIDs[edge.From] {
			return fmt.Errorf("dsl: edges[%d] from %q does not reference a valid node id", i, edge.From)
		}
		if !nodeIDs[edge.To] {
			return fmt.Errorf("dsl: edges[%d] to %q does not reference a valid node id", i, edge.To)
		}
		if edge.From == edge.To {
			return fmt.Errorf("dsl: edges[%d] self-loop on node %q", i, edge.From)
		}
		for _, destField := range edge.Map {
			if _, ok := d.Schema[destField]; !ok {
				return fmt.Errorf("dsl: edges[%d] map destination field %q is not defined in schema", i, destField)
			}
		}
	}

	nodeIncomingFields := buildNodeIncomingFields(d.Edges, d.Schema)
	for _, node := range d.Nodes {
		extraInputs := nodeIncomingFields[node.ID]
		for j, rule := range node.Rules {
			for k, cond := range rule.When {
				if err := validateCondition(fmt.Sprintf("node %q rule %q condition[%d]", node.ID, rule.ID, k), cond, d.Schema, extraInputs); err != nil {
					return fmt.Errorf("dsl: nodes[%d] %w", j, err)
				}
			}
		}
	}

	return nil
}

// ParseAndValidateDraft parses and applies lenient draft validation.
func ParseAndValidateDraft(data []byte) (*DSL, error) {
	d, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := ValidateDraft(d); err != nil {
		return nil, err
	}
	return d, nil
}

func validateFieldDef(loc, name string, fd FieldDef) error {
	switch fd.Type {
	case FieldTypeNumber, FieldTypeString, FieldTypeBoolean:
		// valid
	case FieldTypeEnum:
		if len(fd.Options) == 0 {
			return fmt.Errorf("dsl: %s field %q is type enum but has no options", loc, name)
		}
	default:
		return fmt.Errorf("dsl: %s field %q has unknown type %q", loc, name, fd.Type)
	}
	switch fd.Direction {
	case FieldDirectionInput, FieldDirectionOutput:
		// valid
	default:
		return fmt.Errorf("dsl: %s field %q has unknown direction %q", loc, name, fd.Direction)
	}
	return nil
}

func validateCondition(loc string, cond Condition, schema map[string]FieldDef, extraInputs map[string]bool) error {
	if cond.Field == "" {
		return fmt.Errorf("dsl: %s missing field", loc)
	}
	fd, ok := schema[cond.Field]
	if !ok {
		return fmt.Errorf("dsl: %s references unknown field %q", loc, cond.Field)
	}
	if fd.Direction != FieldDirectionInput && !extraInputs[cond.Field] {
		return fmt.Errorf("dsl: %s field %q is not an input field", loc, cond.Field)
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
