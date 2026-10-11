package query

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/flanksource/uir/query/sqlsyntax"
	"github.com/flanksource/uir/storage/symbolhandle"
)

type SQLFunction struct {
	Name      string   `json:"name"`
	Arguments int      `json:"arguments"`
	Options   []string `json:"options"`
	Terminal  bool     `json:"terminal"`
	Depth     int      `json:"depth,omitempty"`
}

var SQLFunctions = []SQLFunction{
	{Name: "definitions", Arguments: 1},
	{Name: "callers", Arguments: 1, Options: []string{"depth"}, Depth: 1},
	{Name: "callees", Arguments: 1, Options: []string{"depth"}, Depth: 1},
	{Name: "references", Arguments: 1}, {Name: "reads", Arguments: 1}, {Name: "writes", Arguments: 1},
	{Name: "methods", Arguments: 1}, {Name: "implementers", Arguments: 1}, {Name: "inheritors", Arguments: 1},
	{Name: "incoming", Arguments: 1, Options: []string{"via"}},
	{Name: "outgoing", Arguments: 1, Options: []string{"via"}},
	{Name: "traverse", Arguments: 1, Options: []string{"direction", "via", "depth"}, Depth: 3},
	{Name: "path", Arguments: 2, Options: []string{"via", "depth"}, Depth: 8, Terminal: true},
	{Name: "graph", Arguments: 1, Options: []string{"direction", "via", "depth", "nodes", "exclude", "access"}, Depth: 2, Terminal: true},
}

type sqlSource struct {
	selector  *Selector
	function  SQLFunction
	arguments []*sqlSource
	options   sqlOptions
	pos       lexer.Position
}

type sqlOptions struct {
	depth, nodes         int
	direction            string
	via, exclude, access []string
}

func sqlError(pos lexer.Position, format string, args ...any) error {
	return &InvalidQueryError{Message: fmt.Sprintf(format, args...), Line: pos.Line, Column: pos.Column, Hint: "Check the SQL field, selector, function, or option at this position."}
}

func bindSQLSource(source *sqlsyntax.Source, kinds symbolhandle.Kinds, nested bool) (*sqlSource, error) {
	bound := &sqlSource{pos: source.Pos}
	if source.Selector != "" {
		selector, err := sqlSelector(source.Selector, kinds)
		if err != nil {
			return nil, sqlError(source.Pos, "%s", err)
		}
		bound.selector = &selector
		return bound, nil
	}
	name := strings.ToLower(source.Name)
	for _, function := range SQLFunctions {
		if function.Name == name {
			bound.function = function
			break
		}
	}
	if bound.function.Name == "" {
		return nil, sqlError(source.Pos, "unknown navigation function %q", source.Name)
	}
	if nested && bound.function.Terminal {
		return nil, sqlError(source.Pos, "%s is a terminal result and cannot be nested", name)
	}
	if len(source.Arguments) != bound.function.Arguments {
		return nil, sqlError(source.Pos, "%s requires %d source arguments", name, bound.function.Arguments)
	}
	for _, argument := range source.Arguments {
		child, err := bindSQLSource(argument, kinds, true)
		if err != nil {
			return nil, err
		}
		bound.arguments = append(bound.arguments, child)
	}
	options, err := bindSQLOptions(source.Options, bound.function)
	if err != nil {
		return nil, err
	}
	bound.options = options
	return bound, nil
}

func sqlSelector(text string, kinds symbolhandle.Kinds) (Selector, error) {
	parts, err := selectorParts(text)
	if err != nil || len(parts) < 2 {
		return Selector{}, fmt.Errorf("invalid selector %q", text)
	}
	prefix := parts[0]
	if prefix == "api" {
		prefix = "endpoint"
	}
	if !slices.Contains([]string{"pkg", "mod", "func", "method", "field", "type", "struct", "var", "kind"}, prefix) {
		if len(parts) != 2 {
			return Selector{}, fmt.Errorf("%s takes one name pattern", prefix)
		}
		if _, err := kinds.Lookup(prefix); err != nil {
			return Selector{}, err
		}
		selector, err := parseSelector("kind:" + prefix + ":" + text[len(parts[0])+1:])
		selector.NameOnly = true
		return selector, err
	}
	return parseSelector(text)
}

func bindSQLOptions(options []*sqlsyntax.Option, function SQLFunction) (sqlOptions, error) {
	bound := sqlOptions{depth: function.Depth, nodes: 150, direction: "outgoing"}
	if function.Name == "graph" {
		bound.direction = "both"
	}
	seen := map[string]bool{}
	for _, option := range options {
		name := strings.ToLower(option.Name)
		if !slices.Contains(function.Options, name) {
			return bound, sqlError(option.Pos, "%s has no option %q", function.Name, name)
		}
		if seen[name] {
			return bound, sqlError(option.Pos, "option %q is repeated", name)
		}
		seen[name] = true
		value := option.Value.Value()
		if name == "depth" || name == "nodes" {
			number, ok := value.(float64)
			upper := 8
			if name == "nodes" {
				upper = 1000
			}
			if !ok || math.Trunc(number) != number || number < 1 || number > float64(upper) {
				return bound, sqlError(option.Pos, "%s must be an integer between 1 and %d", name, upper)
			}
			if name == "depth" {
				bound.depth = int(number)
			} else {
				bound.nodes = int(number)
			}
			continue
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return bound, sqlError(option.Pos, "%s requires a nonempty string", name)
		}
		switch name {
		case "direction":
			if text != "incoming" && text != "outgoing" && (function.Name != "graph" || text != "both") {
				return bound, sqlError(option.Pos, "invalid %s direction %q", function.Name, text)
			}
			bound.direction = text
		case "via":
			bound.via = sqlList(text)
		case "exclude":
			bound.exclude = sqlList(text)
		case "access":
			bound.access = sqlList(text)
			for _, kind := range bound.access {
				if !slices.Contains([]string{"call", "read", "write"}, kind) {
					return bound, sqlError(option.Pos, "unknown graph access %q", kind)
				}
			}
		}
	}
	if seen["via"] && seen["access"] {
		return bound, sqlError(options[0].Pos, "via and access are mutually exclusive")
	}
	for _, values := range [][]string{bound.via, bound.exclude, bound.access} {
		for _, value := range values {
			if value == "" {
				return bound, sqlError(options[0].Pos, "option list contains an empty item")
			}
		}
	}
	return bound, nil
}

func sqlList(text string) []string {
	parts := strings.Split(text, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
