package uir

import "fmt"

func Find[T Node](tree Node) FindOptions[T] {
	var empty T
	options := FindOptions[T]{
		Root: tree,
	}
	if empty.GetType() != "" {
		options.Include = append(options.Include, empty.GetType())
	}
	return options
}

func (find FindOptions[T]) One() (T, []Warning, error) {
	var zero T
	results, warnings, err := find.WithLimit(1).Many()
	if err != nil || len(results) == 0 {
		return zero, warnings, err
	}
	return results[0], warnings, nil
}

// Validate reports a Name or Language expression that fails to parse.
func (find FindOptions[T]) Validate() error {
	if err := find.Name.Validate(); err != nil {
		return fmt.Errorf("find name: %w", err)
	}
	if err := find.Language.Validate(); err != nil {
		return fmt.Errorf("find language: %w", err)
	}
	return nil
}

// Many returns the matching nodes, and a warning for each node that matched
// but is not a T. It fails before walking when the options do not Validate.
func (find FindOptions[T]) Many() ([]T, []Warning, error) {
	if err := find.Validate(); err != nil {
		return nil, nil, err
	}
	results := []T{}
	var warnings []Warning
	_ = NodeTree{Node: find.Root}.Walk(func(node Node) bool {
		if find.Matches(node) {
			if v, ok := node.(T); ok {
				results = append(results, v)
			} else {
				warnings = append(warnings, Warning{Message: "uir: node matched the filter but has the wrong type", Node: node})
			}
			if find.Limit > 0 && len(results) >= find.Limit {
				return false
			}
		}
		return true
	}, WalkOptions{Depth: find.Depth})
	return results, warnings, nil
}

type FindOptions[T Node] struct {
	Root Node
	// Node types to include in the results
	Include []NodeType `tag:"include"`
	// Node types to exclude from the results
	Exclude []NodeType `tag:"exclude"`
	// Custom filter function to include nodes. It is a runtime-only closure, and
	// FindOptions reaches JSON through Violation.QueryOptions, so it must stay out
	// of the encoding — a non-nil func fails the whole marshal, not just this field.
	Filter NodeFilter `json:"-"`

	Language MatchExpression `tag:"language"`

	// Maximum search depth, 0 means unlimited
	Depth int `tag:"depth"`

	// Maximum number of results to return, 0 means no limit
	Limit int `tag:"limit"`

	// Search by name, supports wildcard "*"
	Name MatchExpression `tag:"name"`
}

func (find FindOptions[T]) AsFilter() NodeFilter {
	return find.Matches
}

func (find FindOptions[T]) Negate() FindOptions[T] {
	newFind := find
	newFind.Filter = func(node Node) bool {
		return !find.Matches(node)
	}

	// Swap include and exclude lists
	newFind.Exclude, newFind.Include = newFind.Include, newFind.Exclude

	return newFind
}

func (find FindOptions[T]) Matches(node Node) bool {
	nodeType := node.GetIdentifier().GetNodeType()
	if len(find.Include) > 0 {
		matched := false
		for _, nt := range find.Include {
			if nodeType == nt {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(find.Exclude) > 0 {
		for _, nt := range find.Exclude {
			if nodeType == nt {
				return false
			}
		}
	}

	if find.Name != "" {
		matched, negated := find.Name.Matches(node.GetIdentifier().GetName())
		pathMatched, pathNegated := find.Name.Matches(node.GetIdentifier().String())

		if negated || pathNegated {
			return false
		}
		return matched || pathMatched
	}

	return true
}

func (find FindOptions[T]) WithFilter(filter func(Node) bool) FindOptions[T] {
	find.Filter = filter
	return find
}

func (find FindOptions[T]) WithLimit(limit int) FindOptions[T] {
	find.Limit = limit
	return find
}

func (find FindOptions[T]) WithName(name string) FindOptions[T] {
	find.Name = MatchExpression(name)
	return find
}

func (find FindOptions[T]) WithDepth(depth int) FindOptions[T] {
	find.Depth = depth
	return find
}
func (find FindOptions[T]) IncludeTypes(types ...NodeType) FindOptions[T] {
	find.Include = append(find.Include, types...)
	return find
}

func (find FindOptions[T]) ExcludeTypes(types ...NodeType) FindOptions[T] {
	find.Exclude = append(find.Exclude, types...)
	return find
}
