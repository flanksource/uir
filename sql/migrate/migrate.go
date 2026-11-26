// Package migrate projects inspected UIR schemas into sqlglot migration inputs
// and overlays selected reference members onto the target schema.
//
// Planning is offline (engine.Plan): no database is read, so the dependents a
// column change forces to be rebuilt (indexes, keys, foreign keys in both
// directions, checks, defaults, statistics) come from the baseline catalogue,
// which is the target. A dependent the target does not hold is invisible to
// the plan.
package migrate

import (
	"fmt"
	"slices"
	"strings"

	// Registers the "sqlserver" dialect with the engine.
	_ "github.com/flanksource/sqlglot-go/database/migrate/dialect/sqlserver"
	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/migrate/engine"
	"github.com/flanksource/uir"
)

const CatalogVersion = 3

// dialect is the engine dialect every UIR catalogue plans with ("tsql" in UIR).
const dialect = "sqlserver"

type Selection struct {
	Kind   string     `json:"kind"`
	Object model.Name `json:"object"`
	Member string     `json:"member,omitempty"`
}

func (s Selection) ID() string {
	id := fmt.Sprintf("%s:%q.%q", s.Kind, s.Object.Schema, s.Object.Name)
	if s.Member != "" {
		id += fmt.Sprintf("/%q", s.Member)
	}
	return id
}

func ParseSelection(id string) (Selection, error) {
	kind, rest, ok := strings.Cut(id, ":")
	s := Selection{Kind: kind}
	if !ok {
		return s, fmt.Errorf("invalid selection ID %q", id)
	}
	switch kind {
	case "table", "view", "procedure", "function", "trigger", "table-type", "sequence":
		_, err := fmt.Sscanf(rest, "%q.%q", &s.Object.Schema, &s.Object.Name)
		if err != nil {
			return s, fmt.Errorf("invalid selection ID %q: %w", id, err)
		}
	case "column", "index", "foreign-key", "check":
		_, err := fmt.Sscanf(rest, "%q.%q/%q", &s.Object.Schema, &s.Object.Name, &s.Member)
		if err != nil || s.Member == "" {
			return s, fmt.Errorf("invalid member selection ID %q", id)
		}
	default:
		return s, fmt.Errorf("unsupported selection kind %q", kind)
	}
	if s.Object.Schema == "" || s.Object.Name == "" || s.ID() != id {
		return s, fmt.Errorf("invalid or noncanonical selection ID %q", id)
	}
	return s, nil
}

// Request plans the migration of Target toward the selected parts of
// Reference. Target is the baseline and must be a complete catalogue. A
// complete Reference plans in complete scope, so selecting a member only the
// target holds drops it. A declared Reference plans in declared scope over the
// objects it declares. A declared table is complete unless it is partial: the
// members a complete one omits can be selected and are dropped, while those of
// a partial one are kept and listed as preserved. An undeclared object is left
// out of the plan and is never dropped.
type Request struct {
	Reference  uir.UIR     `json:"reference"`
	Target     uir.UIR     `json:"target"`
	Selections []Selection `json:"selections"`
}

// Select applies the selections to the target and returns the engine request
// that plans the difference, after planning it once to prove the selected
// desired schema is valid.
func Select(request Request) (engine.PlanRequest, error) {
	input, _, err := plan(request)
	return input, err
}

func plan(request Request) (engine.PlanRequest, engine.Artifact, error) {
	reference, err := Schema(request.Reference)
	if err != nil {
		return engine.PlanRequest{}, engine.Artifact{}, fmt.Errorf("reference: %w", err)
	}
	target, err := Schema(request.Target)
	if err != nil {
		return engine.PlanRequest{}, engine.Artifact{}, fmt.Errorf("target: %w", err)
	}
	input, err := planRequest(request, reference, target)
	if err != nil {
		return input, engine.Artifact{}, err
	}
	artifact, err := engine.Plan(input)
	if err != nil {
		return input, artifact, fmt.Errorf("selected desired schema: %w", err)
	}
	return input, artifact, nil
}

func planRequest(request Request, reference, target model.Schema) (engine.PlanRequest, error) {
	scope, err := planScope(request.Reference.SQLCatalog, request.Target.SQLCatalog, reference)
	if err != nil {
		return engine.PlanRequest{}, err
	}
	declared := scope.Mode == engine.ScopeDeclared
	if declared {
		if err := requireDeclared(reference, request.Selections); err != nil {
			return engine.PlanRequest{}, err
		}
	}
	desired, err := overlay(reference, target, request.Selections)
	if err != nil {
		return engine.PlanRequest{}, err
	}
	input := engine.PlanRequest{Dialect: dialect, Scope: scope, Current: target, Desired: desired}
	if declared {
		keepPartial(&input.Desired, reference)
		input.Context = unmanaged(target, scope.Objects)
	}
	return input, nil
}

// keepPartial marks the desired tables the reference declares partial, so the
// planner keeps the members those tables do not name instead of dropping them.
func keepPartial(desired *model.Schema, reference model.Schema) {
	for i := range desired.Tables {
		at := slices.IndexFunc(reference.Tables, func(t model.Table) bool { return t.Name == desired.Tables[i].Name })
		if at >= 0 {
			desired.Tables[i].Partial = reference.Tables[at].Partial
		}
	}
}
