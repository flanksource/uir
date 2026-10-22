// Package migrate projects inspected UIR schemas into sqlglot migration inputs
// and overlays selected reference members onto the target schema.
package migrate

import (
	"fmt"
	"strings"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/migrate/sqlserver"
	"github.com/flanksource/uir"
)

const CatalogVersion = 1

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
	case "table", "view", "procedure", "function", "trigger":
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

type Request struct {
	Reference  uir.UIR     `json:"reference"`
	Target     uir.UIR     `json:"target"`
	Selections []Selection `json:"selections"`
}

func Plan(request Request) (model.Plan, error) {
	input, err := Select(request)
	if err != nil {
		return model.Plan{}, err
	}
	return sqlserver.Plan(input)
}

func Select(request Request) (model.Request, error) {
	reference, err := Schema(request.Reference)
	if err != nil {
		return model.Request{}, fmt.Errorf("reference: %w", err)
	}
	target, err := Schema(request.Target)
	if err != nil {
		return model.Request{}, fmt.Errorf("target: %w", err)
	}
	if err := sameScopes(request.Reference.SQLCatalog, request.Target.SQLCatalog); err != nil {
		return model.Request{}, err
	}
	desired, err := overlay(reference, target, request.Selections)
	if err != nil {
		return model.Request{}, err
	}
	input := model.Request{Current: target, Desired: desired}
	if _, err := sqlserver.Plan(input); err != nil {
		return model.Request{}, fmt.Errorf("selected desired schema: %w", err)
	}
	return input, nil
}
