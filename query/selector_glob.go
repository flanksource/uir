package query

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const maximumSelectorPattern = 512

func parseSelector(value string) (Selector, error) {
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 || parts[1] == "" {
		return Selector{}, fmt.Errorf("invalid typed selector %q", value)
	}
	selector := Selector{Kind: parts[0], Pattern: parts[1]}
	if len(parts) == 3 {
		if selector.Kind != "pkg" || parts[2] == "" {
			return Selector{}, fmt.Errorf("invalid typed selector %q", value)
		}
		selector.ModulePattern, selector.Pattern = parts[1], parts[2]
		if selector.Pattern != "." && slices.Contains(strings.Split(selector.Pattern, "/"), ".") {
			return Selector{}, fmt.Errorf("invalid typed selector %q: . is only valid as the whole relative package pattern", value)
		}
	}
	for _, pattern := range []string{selector.Pattern, selector.ModulePattern} {
		if pattern == "" {
			continue
		}
		if _, err := compileSelectorGlob(pattern); err != nil {
			return Selector{}, fmt.Errorf("invalid typed selector %q: %w", value, err)
		}
	}
	return selector, nil
}

type selectorGlob struct {
	segments []*regexp.Regexp
	stars    []bool
}

func compileSelectorGlob(pattern string) (selectorGlob, error) {
	if pattern == "" || len(pattern) > maximumSelectorPattern {
		return selectorGlob{}, fmt.Errorf("glob length must be between 1 and %d", maximumSelectorPattern)
	}
	parts := strings.Split(pattern, "/")
	glob := selectorGlob{segments: make([]*regexp.Regexp, len(parts)), stars: make([]bool, len(parts))}
	for position, part := range parts {
		if part == "" {
			return selectorGlob{}, fmt.Errorf("empty glob path segment")
		}
		if part == "**" {
			glob.stars[position] = true
			continue
		}
		var expression strings.Builder
		expression.WriteByte('^')
		for i := 0; i < len(part); i++ {
			switch part[i] {
			case '*':
				if i+1 < len(part) && part[i+1] == '*' {
					return selectorGlob{}, fmt.Errorf("** must occupy a complete path segment")
				}
				expression.WriteString("[^/]*")
			case '?':
				expression.WriteString("[^/]")
			case '\\':
				i++
				if i == len(part) {
					return selectorGlob{}, fmt.Errorf("dangling glob escape")
				}
				expression.WriteString(regexp.QuoteMeta(part[i : i+1]))
			default:
				expression.WriteString(regexp.QuoteMeta(part[i : i+1]))
			}
		}
		expression.WriteByte('$')
		glob.segments[position] = regexp.MustCompile(expression.String())
	}
	return glob, nil
}

func (glob selectorGlob) matches(value string) bool {
	if value == "." {
		return len(glob.stars) == 1 && glob.stars[0] || len(glob.segments) == 1 && glob.segments[0] != nil && glob.segments[0].String() == `^\.$`
	}
	parts := strings.Split(value, "/")
	memo := map[[2]int]bool{}
	seen := map[[2]int]bool{}
	var walk func(int, int) bool
	walk = func(pattern, path int) bool {
		state := [2]int{pattern, path}
		if seen[state] {
			return memo[state]
		}
		seen[state] = true
		if pattern == len(glob.segments) {
			memo[state] = path == len(parts)
			return memo[state]
		}
		if glob.stars[pattern] {
			memo[state] = walk(pattern+1, path) || path < len(parts) && walk(pattern, path+1)
			return memo[state]
		}
		memo[state] = path < len(parts) && glob.segments[pattern].MatchString(parts[path]) && walk(pattern+1, path+1)
		return memo[state]
	}
	return walk(0, 0)
}
