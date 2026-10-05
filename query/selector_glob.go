package query

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const maximumSelectorPattern = 512

func parseSelector(value string) (Selector, error) {
	parts, err := selectorParts(value)
	if err != nil {
		return Selector{}, fmt.Errorf("invalid typed selector %q: %w", value, err)
	}
	if len(parts) < 2 || len(parts) > 3 || parts[1] == "" {
		return Selector{}, fmt.Errorf("invalid typed selector %q", value)
	}
	selector := Selector{Kind: parts[0], Pattern: parts[1]}
	if selector.Kind == "kind" {
		// kind:<registered kind>[:<name pattern>]
		name, literal := globLiteral(parts[1])
		if !literal {
			return Selector{}, fmt.Errorf("invalid typed selector %q: the kind name must not be a glob", value)
		}
		selector.SymbolKind, selector.Pattern = name, "*"
		if len(parts) == 3 {
			if parts[2] == "" {
				return Selector{}, fmt.Errorf("invalid typed selector %q", value)
			}
			selector.Pattern = parts[2]
		}
	} else if len(parts) == 3 {
		if selector.Kind != "pkg" || parts[2] == "" {
			return Selector{}, fmt.Errorf("invalid typed selector %q", value)
		}
		selector.ModulePattern, selector.Pattern = parts[1], parts[2]
		if selector.Pattern != "." && slices.Contains(strings.Split(selector.Pattern, "/"), ".") {
			return Selector{}, fmt.Errorf("invalid typed selector %q: . is only valid as the whole relative package pattern", value)
		}
	}
	for _, pattern := range []*string{&selector.Pattern, &selector.ModulePattern} {
		if strings.HasSuffix(*pattern, "/...") {
			*pattern = strings.TrimSuffix(*pattern, "...") + "**"
		}
		if *pattern == "" {
			continue
		}
		if _, err := compileSelectorGlob(*pattern); err != nil {
			return Selector{}, fmt.Errorf("invalid typed selector %q: %w", value, err)
		}
	}
	return selector, nil
}

// parseEntityField reads an Entity:Field reference, such as Plan:PlanField1, as a field selector
// whose owner glob matches the name of the field's direct owner and whose pattern its own name.
func parseEntityField(value string) (Selector, error) {
	parts, err := selectorParts(value)
	if err != nil {
		return Selector{}, fmt.Errorf("invalid Entity:Field reference %q: %w", value, err)
	}
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Selector{}, fmt.Errorf("invalid Entity:Field reference %q: expected <entity>:<field>", value)
	}
	selector := Selector{Kind: "field", Owner: parts[0], Pattern: parts[1]}
	for _, pattern := range []string{selector.Owner, selector.Pattern} {
		if _, err := compileSelectorGlob(pattern); err != nil {
			return Selector{}, fmt.Errorf("invalid Entity:Field reference %q: %w", value, err)
		}
	}
	return selector, nil
}

// selectorParts splits a selector's text at the colons outside double quotes and escapes. A part is
// unquoted or one complete quoted string; the quotes are removed and every escape is kept for the
// glob, where \ makes the next character literal.
func selectorParts(value string) ([]string, error) {
	parts, err := splitSelector(value, false)
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(parts))
	for i, part := range parts {
		texts[i] = part.text
	}
	return texts, nil
}

// selectorPart is one colon-separated part of a selector: its glob text without quotes, and whether
// it was quoted.
type selectorPart struct {
	text   string
	quoted bool
}

// splitSelector splits a selector at the colons outside quotes and escapes. A partial selector, one
// still being typed, may end inside a quote or after a lone backslash, which is dropped.
func splitSelector(value string, partial bool) ([]selectorPart, error) {
	var parts []selectorPart
	var part strings.Builder
	quoted, closed, opened := false, false, false
	for i := 0; i < len(value); i++ {
		character := value[i]
		switch {
		case character == ':' && !quoted:
			parts = append(parts, selectorPart{text: part.String(), quoted: opened})
			part.Reset()
			closed, opened = false, false
		case closed:
			return nil, fmt.Errorf("text follows a closing quote")
		case character == '\\' && i+1 == len(value):
			if !partial {
				return nil, fmt.Errorf("dangling escape")
			}
		case character == '\\':
			part.WriteString(value[i : i+2])
			i++
		case character == '"' && quoted:
			quoted, closed = false, true
		case character == '"' && part.Len() == 0:
			quoted, opened = true, true
		case character == '"':
			return nil, fmt.Errorf("a quote must enclose a whole value")
		default:
			part.WriteByte(character)
		}
	}
	if quoted && !partial {
		return nil, fmt.Errorf("unterminated quote")
	}
	return append(parts, selectorPart{text: part.String(), quoted: opened}), nil
}

// QuoteSelectorValue spells a literal as one value of a typed selector, such as the name in
// kind:acme.screen:<value> or the path in pkg:<value>, matching exactly that literal:
//
//   - A literal made only of letters, digits and _ . / @ # $ ! - is returned unchanged.
//   - Any other literal, one with a space, a colon, a quote, a parenthesis or a glob character, is
//     wrapped in double quotes with \, ", * and ? escaped by a backslash. Inside the quotes a colon
//     does not split the selector, and an escaped * or ? matches itself rather than globbing.
//   - / is never escaped: in every selector value it separates path segments, which a pattern matches
//     one by one, so a quoted module key or package path with spaces, such as
//     "Acme/Digital Funeral/rules", still matches segment by segment, and appending /... before
//     quoting still selects the path and everything below it.
//
// A symbol selector (func:, method:, field:, struct:, type:, var:, kind:) matches its value against a
// spelling chosen by the characters the value holds, as compact_selector.go's selectorKey reads it:
// a value without . or / against the symbol's own name; one with . but no / against its name within
// its package, Owner.Name for a member; and one with / against its whole query name, package path
// included, such as Acme/Ins/screens.PolicyScreen. A symbol whose own name holds a / is therefore
// selected by quoting its query name, never its bare name. An Entity:Field reference quotes the
// entity's name and the field's name separately and matches each against those names alone.
func QuoteSelectorValue(literal string) string {
	if plainSelectorValue.MatchString(literal) {
		return literal
	}
	return `"` + globEscaper.Replace(literal) + `"`
}

var (
	plainSelectorValue = regexp.MustCompile(`^[A-Za-z0-9_./@#$!-]+$`)
	globEscaper        = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `*`, `\*`, `?`, `\?`)
)

// globLiteral is the text a glob pattern matches when it has no unescaped * or ?, with its escapes
// removed; false for a pattern with a wildcard.
func globLiteral(pattern string) (string, bool) {
	var literal strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*', '?':
			return "", false
		case '\\':
			if i+1 < len(pattern) {
				i++
			}
		}
		literal.WriteByte(pattern[i])
	}
	return literal.String(), true
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
