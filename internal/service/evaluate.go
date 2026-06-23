package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/domain/dsl"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

// Evaluate loads the latest published DSL for the given ruleset and runs input
// through it. If no published version exists yet, falls back to the saved draft
// so the simulator works during development before the first publish.
func (s *RulesetService) Evaluate(ctx context.Context, workspace, key string, input map[string]any) (*domain.EvalResult, error) {
	var dslBytes []byte

	v, latestErr := s.db.GetLatestVersion(ctx, workspace, key)
	switch {
	case latestErr == nil:
		var blobErr error
		dslBytes, blobErr = s.blobs.GetDSL(ctx, workspace, key, v.Version)
		if blobErr != nil {
			return nil, mapErr(blobErr)
		}
	case errors.Is(latestErr, port.ErrNotFound):
		// No published version yet — fall back to the draft so the simulator
		// works during development before the first publish.
		draft, draftErr := s.db.GetDraft(ctx, workspace, key)
		if draftErr != nil {
			return nil, mapErr(draftErr)
		}
		dslBytes = []byte(draft.DSL)
	default:
		return nil, mapErr(latestErr)
	}

	d, err := dsl.ParseAndValidate(dslBytes)
	if err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}

	result, trace, err := evalDSL(d, input)
	if err != nil {
		return nil, &ValidationError{Msg: err.Error()}
	}
	return &domain.EvalResult{Result: result, Trace: trace}, nil
}


// evalDSL runs a parsed DSL against input facts and returns output + trace.
func evalDSL(d *dsl.DSL, input map[string]any) (map[string]any, []domain.TraceEntry, error) {
	nodeByID := make(map[string]*dsl.RuleNode, len(d.Nodes))
	for i := range d.Nodes {
		nodeByID[d.Nodes[i].ID] = &d.Nodes[i]
	}
	edgesFrom := make(map[string][]dsl.Edge)
	for _, e := range d.Edges {
		edgesFrom[e.From] = append(edgesFrom[e.From], e)
	}

	facts := make(map[string]any, len(input))
	for k, v := range input {
		facts[k] = v
	}
	return evalFrom(d.Entry, d, nodeByID, edgesFrom, facts)
}

func evalFrom(
	nodeID string,
	d *dsl.DSL,
	nodeByID map[string]*dsl.RuleNode,
	edgesFrom map[string][]dsl.Edge,
	facts map[string]any,
) (map[string]any, []domain.TraceEntry, error) {
	output := make(map[string]any)
	var trace []domain.TraceEntry

	for {
		node, ok := nodeByID[nodeID]
		if !ok {
			return nil, nil, fmt.Errorf("node %q not found", nodeID)
		}

		nodeOut, nodeTrace, err := evalNode(node, d, facts)
		if err != nil {
			return nil, nil, err
		}
		trace = append(trace, nodeTrace...)
		for k, v := range nodeOut {
			output[k] = v
			facts[k] = v
		}

		outEdges := edgesFrom[nodeID]
		if len(outEdges) == 0 {
			break
		}
		if len(outEdges) == 1 {
			edge := outEdges[0]
			for k, v := range remapEdge(edge, nodeOut) {
				facts[k] = v
			}
			nodeID = edge.To
		} else {
			for _, edge := range outEdges {
				subFacts := copyFacts(facts)
				for k, v := range remapEdge(edge, nodeOut) {
					subFacts[k] = v
				}
				subOut, subTrace, err := evalFrom(edge.To, d, nodeByID, edgesFrom, subFacts)
				if err != nil {
					return nil, nil, err
				}
				trace = append(trace, subTrace...)
				for k, v := range subOut {
					output[k] = v
				}
			}
			break
		}
	}
	return output, trace, nil
}

func evalNode(node *dsl.RuleNode, d *dsl.DSL, facts map[string]any) (map[string]any, []domain.TraceEntry, error) {
	output := make(map[string]any)
	var trace []domain.TraceEntry

	switch node.Strategy {
	case dsl.StrategyFirstMatch:
		for _, rule := range node.Rules {
			matched, err := evalConditions(rule.When, node, &rule, d, facts)
			if err != nil {
				return nil, nil, err
			}
			trace = append(trace, domain.TraceEntry{RuleID: rule.ID, RuleName: rule.Name, NodeID: node.ID, Matched: matched})
			if matched {
				for k, v := range rule.Then {
					output[k] = v
				}
				return output, trace, nil
			}
		}

	case dsl.StrategyAllMatches:
		anyMatched := false
		for _, rule := range node.Rules {
			matched, err := evalConditions(rule.When, node, &rule, d, facts)
			if err != nil {
				return nil, nil, err
			}
			trace = append(trace, domain.TraceEntry{RuleID: rule.ID, RuleName: rule.Name, NodeID: node.ID, Matched: matched})
			if matched {
				anyMatched = true
				for k, v := range rule.Then {
					output[k] = v
				}
			}
		}
		if anyMatched {
			return output, trace, nil
		}
	}

	if node.Default != nil {
		for k, v := range node.Default {
			output[k] = v
		}
	}
	return output, trace, nil
}

func evalConditions(conditions []dsl.Condition, node *dsl.RuleNode, rule *dsl.Rule, d *dsl.DSL, facts map[string]any) (bool, error) {
	for _, cond := range conditions {
		ok, err := evalCondition(cond, node, rule, d, facts)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func evalCondition(cond dsl.Condition, node *dsl.RuleNode, rule *dsl.Rule, d *dsl.DSL, facts map[string]any) (bool, error) {
	fd := d.Schema[cond.Field]
	raw := facts[cond.Field]

	switch fd.Type {
	case dsl.FieldTypeNumber:
		return evalNumber(cond, raw)
	case dsl.FieldTypeString:
		return evalString(cond, raw)
	case dsl.FieldTypeBoolean:
		return evalBool(cond, raw)
	case dsl.FieldTypeEnum:
		return evalEnum(cond, raw)
	}
	return false, fmt.Errorf("node %s rule %s: unknown field type %q", node.ID, rule.ID, fd.Type)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case nil:
		return 0, true
	}
	return 0, false
}

func evalNumber(cond dsl.Condition, raw any) (bool, error) {
	fv, ok := toFloat(raw)
	if !ok {
		return false, fmt.Errorf("field %q: expected number, got %T", cond.Field, raw)
	}
	cv, ok := toFloat(cond.Value)
	if !ok {
		return false, fmt.Errorf("field %q: condition value is not a number", cond.Field)
	}
	switch cond.Op {
	case "eq":
		return fv == cv, nil
	case "ne":
		return fv != cv, nil
	case "gt":
		return fv > cv, nil
	case "gte":
		return fv >= cv, nil
	case "lt":
		return fv < cv, nil
	case "lte":
		return fv <= cv, nil
	}
	return false, fmt.Errorf("field %q: unsupported number operator %q", cond.Field, cond.Op)
}

func evalString(cond dsl.Condition, raw any) (bool, error) {
	var fv string
	switch x := raw.(type) {
	case string:
		fv = x
	case nil:
		fv = ""
	default:
		return false, fmt.Errorf("field %q: expected string, got %T", cond.Field, raw)
	}
	cv, ok := cond.Value.(string)
	if !ok {
		return false, fmt.Errorf("field %q: condition value is not a string", cond.Field)
	}
	switch cond.Op {
	case "eq":
		return fv == cv, nil
	case "ne":
		return fv != cv, nil
	case "contains":
		return strings.Contains(fv, cv), nil
	case "starts_with":
		return strings.HasPrefix(fv, cv), nil
	case "ends_with":
		return strings.HasSuffix(fv, cv), nil
	}
	return false, fmt.Errorf("field %q: unsupported string operator %q", cond.Field, cond.Op)
}

func evalBool(cond dsl.Condition, raw any) (bool, error) {
	var fv bool
	switch x := raw.(type) {
	case bool:
		fv = x
	case nil:
		fv = false
	default:
		return false, fmt.Errorf("field %q: expected boolean, got %T", cond.Field, raw)
	}
	cv, ok := cond.Value.(bool)
	if !ok {
		return false, fmt.Errorf("field %q: condition value is not a boolean", cond.Field)
	}
	switch cond.Op {
	case "eq":
		return fv == cv, nil
	case "ne":
		return fv != cv, nil
	}
	return false, fmt.Errorf("field %q: unsupported boolean operator %q", cond.Field, cond.Op)
}

func evalEnum(cond dsl.Condition, raw any) (bool, error) {
	var fv string
	switch x := raw.(type) {
	case string:
		fv = x
	case nil:
		fv = ""
	default:
		return false, fmt.Errorf("field %q: expected string (enum), got %T", cond.Field, raw)
	}
	switch cond.Op {
	case "eq":
		return fv == cond.Value.(string), nil
	case "ne":
		return fv != cond.Value.(string), nil
	case "in":
		for _, s := range cond.Value.([]any) {
			if fv == s.(string) {
				return true, nil
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("field %q: unsupported enum operator %q", cond.Field, cond.Op)
}

func remapEdge(edge dsl.Edge, nodeOut map[string]any) map[string]any {
	result := make(map[string]any)
	if len(edge.Map) == 0 {
		for k, v := range nodeOut {
			result[k] = v
		}
		return result
	}
	for outField, inField := range edge.Map {
		if v, ok := nodeOut[outField]; ok {
			result[inField] = v
		}
	}
	return result
}

func copyFacts(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
