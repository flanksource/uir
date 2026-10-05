package query

import "fmt"

type StructuredOptions struct {
	Kinds     []string
	Include   []string
	Exclude   []string
	Relations []string
}

func ParseStructured(input string, options StructuredOptions) (*Expr, error) {
	var base *Expr
	if input != "" {
		parsed, err := Parse(input)
		if err != nil {
			return nil, err
		}
		base = parsed.Expr
	}
	if base == nil && len(options.Kinds)+len(options.Include)+len(options.Exclude)+len(options.Relations) == 0 {
		return nil, &InvalidQueryError{Message: "query expression or structured flag is required", Hint: "Enter an expression or select a node kind or relation."}
	}
	expression, err := Compose(base, options)
	if err != nil {
		return nil, &InvalidQueryError{Message: err.Error(), Hint: "Check the structured query flags.", Cause: err}
	}
	return expression, nil
}

func Compose(base *Expr, options StructuredOptions) (*Expr, error) {
	structured := len(options.Kinds)+len(options.Include)+len(options.Exclude)+len(options.Relations) > 0
	if base != nil && !structured {
		return base, nil
	}
	if base == nil && len(options.Kinds) == 0 && len(options.Relations) > 0 {
		var combined *Expr
		for _, relation := range options.Relations {
			kind := map[string]string{"callers": "func", "calls": "func", "implements": "type", "inherits": "type"}[relation]
			if kind == "" {
				return nil, fmt.Errorf("unknown structured query relation %q", relation)
			}
			step, err := Compose(nil, StructuredOptions{Kinds: []string{kind}, Include: options.Include, Exclude: options.Exclude, Relations: []string{relation}})
			if err != nil {
				return nil, err
			}
			if combined == nil {
				combined = step
			} else {
				combined = &Expr{Kind: ExprUnion, Left: combined, Right: step}
			}
		}
		return combined, nil
	}
	if base != nil && base.Kind == ExprPath {
		return nil, fmt.Errorf("a call path cannot be used as a starting set for structured flags")
	}
	if len(options.Kinds) > 0 {
		var kinds *Expr
		for _, kind := range options.Kinds {
			switch kind {
			case "func", "method", "var", "type", "module", "package":
			default:
				return nil, fmt.Errorf("unknown structured query kind %q", kind)
			}
			selector := &Expr{Kind: ExprSelector, Selector: &Selector{Kind: kind, Pattern: "*"}}
			if kinds == nil {
				kinds = selector
			} else {
				kinds = &Expr{Kind: ExprUnion, Left: kinds, Right: selector}
			}
		}
		if base == nil {
			base = kinds
		} else {
			base = &Expr{Kind: ExprIntersection, Left: base, Right: kinds}
		}
	}
	if base == nil {
		base = &Expr{Kind: ExprSelector, Selector: &Selector{Kind: "all", Pattern: "*"}}
	}
	if len(options.Include)+len(options.Exclude) > 0 {
		filtered := &Expr{Kind: ExprModifier, Left: base}
		for _, group := range []struct {
			patterns []string
			include  bool
		}{{options.Include, true}, {options.Exclude, false}} {
			for _, pattern := range group.patterns {
				selector, err := parseSelector("path:" + pattern)
				if err != nil {
					return nil, err
				}
				filtered.Modifiers = append(filtered.Modifiers, TypedModifier{Include: group.include, Selector: selector})
			}
		}
		base = filtered
	}
	if len(options.Relations) == 0 {
		return base, nil
	}
	var combined *Expr
	for _, relation := range options.Relations {
		operator := map[string]string{"callers": "<", "calls": ">", "implements": ":impl", "inherits": ":inherits"}[relation]
		if operator == "" {
			return nil, fmt.Errorf("unknown structured query relation %q", relation)
		}
		step := &Expr{Kind: ExprRelation, Relation: operator, Left: base}
		if combined == nil {
			combined = step
		} else {
			combined = &Expr{Kind: ExprUnion, Left: combined, Right: step}
		}
	}
	return combined, nil
}
