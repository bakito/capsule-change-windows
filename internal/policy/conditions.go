// Copyright 2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"k8s.io/apimachinery/pkg/util/validation"

	api "github.com/projectcapsule/schedule-action-controller/api/v1alpha1"
)

// ConditionBudget bounds all evaluations in one admission request. Denials and
// warnings use separate budgets so a broken warning cannot exhaust denial work.
const (
	ConditionBudget     int64  = 100000
	expressionCostLimit uint64 = 10000
)

type condition struct {
	name    string
	program cel.Program
}

// A compiler belongs to one policy compilation, never to an admission request.
// Its immutable programs are published in the existing generation-aware Store.
type conditionCompiler struct{ env *cel.Env }

func (c *conditionCompiler) compile(conditions []api.MatchCondition) ([]condition, error) {
	if len(conditions) > 64 {
		return nil, fmt.Errorf("matchConditions allows at most 64 entries")
	}
	if len(conditions) == 0 {
		return nil, nil
	}
	if c.env == nil {
		env, err := cel.NewEnv(
			cel.Variable("object", cel.DynType), cel.Variable("oldObject", cel.DynType),
			cel.Variable("request", cel.DynType), cel.Variable("namespaceObject", cel.DynType),
			cel.CrossTypeNumericComparisons(true),
			cel.ASTValidators(cel.ValidateRegexLiterals(), cel.ValidateComprehensionNestingLimit(2)),
		)
		if err != nil {
			return nil, err
		}
		c.env = env
	}
	compiled := make([]condition, 0, len(conditions))
	seen := make(map[string]bool, len(conditions))
	for _, entry := range conditions {
		if len(validation.IsDNS1123Label(entry.Name)) != 0 || seen[entry.Name] {
			return nil, fmt.Errorf("matchCondition names must be unique DNS labels")
		}
		seen[entry.Name] = true
		if strings.TrimSpace(entry.Expression) == "" || len(entry.Expression) > 4096 {
			return nil, fmt.Errorf("matchCondition %s requires 1 to 4096 expression bytes", entry.Name)
		}
		ast, issues := c.env.Compile(entry.Expression)
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("matchCondition %s: %w", entry.Name, issues.Err())
		}
		if ast.OutputType() != cel.BoolType {
			return nil, fmt.Errorf("matchCondition %s must return bool", entry.Name)
		}
		program, err := c.env.Program(ast, cel.CostLimit(expressionCostLimit), cel.InterruptCheckFrequency(100), cel.EvalOptions(cel.OptOptimize))
		if err != nil {
			return nil, fmt.Errorf("matchCondition %s: %w", entry.Name, err)
		}
		compiled = append(compiled, condition{name: entry.Name, program: program})
	}
	return compiled, nil
}

func (m Message) HasConditions() bool { return len(m.conditions) > 0 }

// MatchConditions follows AND semantics, including false taking precedence over
// an ordinary evaluation error. Request-budget exhaustion and cancellation abort evaluation.
func (m Message) MatchConditions(ctx context.Context, variables map[string]any, budget *int64) (bool, error) {
	var firstErr error
	for _, condition := range m.conditions {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if *budget <= 0 {
			return false, fmt.Errorf("matchConditions cost budget exhausted")
		}
		value, details, err := condition.program.ContextEval(ctx, variables)
		if details == nil || details.ActualCost() == nil {
			return false, fmt.Errorf("matchCondition %s: evaluation cost unavailable", condition.name)
		}
		*budget -= int64(*details.ActualCost())
		if *budget < 0 {
			return false, fmt.Errorf("matchConditions cost budget exhausted")
		}
		if value == types.False {
			return false, nil
		}
		if err != nil || value != types.True {
			if firstErr == nil {
				firstErr = fmt.Errorf("matchCondition %s did not evaluate to true: %v", condition.name, err)
			}
		}
	}
	return firstErr == nil, firstErr
}
