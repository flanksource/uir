package query

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type queryToken struct {
	kind, value string
}

type parserState struct {
	tokens []queryToken
}

func (state *parserState) addToken(kind, value string) {
	state.tokens = append(state.tokens, queryToken{kind, value})
}

type expressionParser struct {
	tokens []queryToken
	index  int
}

func (parser *expressionParser) take(kind string) (queryToken, bool) {
	if parser.index >= len(parser.tokens) || parser.tokens[parser.index].kind != kind {
		return queryToken{}, false
	}
	token := parser.tokens[parser.index]
	parser.index++
	return token, true
}

func (parser *expressionParser) expression() (*Expr, error) {
	left, err := parser.union()
	if err != nil {
		return nil, err
	}
	if token, ok := parser.take("path"); ok {
		right, err := parser.union()
		if err != nil {
			return nil, err
		}
		depth, err := relationDepth(token.value, 8)
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: ExprPath, Left: left, Right: right, Depth: depth}
	}
	if parser.index != len(parser.tokens) {
		return nil, fmt.Errorf("unexpected token %q", parser.tokens[parser.index].value)
	}
	return left, nil
}

func (parser *expressionParser) union() (*Expr, error) {
	left, err := parser.intersection()
	if err != nil {
		return nil, err
	}
	for {
		if _, ok := parser.take("or"); !ok {
			return left, nil
		}
		right, err := parser.intersection()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: ExprUnion, Left: left, Right: right}
	}
}

func (parser *expressionParser) intersection() (*Expr, error) {
	left, err := parser.chain()
	if err != nil {
		return nil, err
	}
	for {
		if _, ok := parser.take("and"); !ok {
			return left, nil
		}
		right, err := parser.chain()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: ExprIntersection, Left: left, Right: right}
	}
}

func (parser *expressionParser) primary() (*Expr, error) {
	var left *Expr
	if token, ok := parser.take("selector"); ok {
		selector, err := parseSelector(token.value)
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: ExprSelector, Selector: &selector}
	} else if token, ok := parser.take("symbol"); ok {
		if err := validateSymbolPattern(token.value); err != nil {
			return nil, err
		}
		left = &Expr{Kind: ExprSymbol, Symbol: token.value}
	} else if _, ok := parser.take("open"); ok {
		var err error
		left, err = parser.union()
		if err != nil {
			return nil, err
		}
		if _, ok := parser.take("close"); !ok {
			return nil, errors.New("missing closing parenthesis")
		}
	} else {
		return nil, errors.New("expected a Go symbol")
	}
	return left, nil
}

func (parser *expressionParser) modifiers(left *Expr) (*Expr, error) {
	for {
		token, ok := parser.take("modifier")
		if !ok {
			return left, nil
		}
		selector, err := parseSelector(token.value[1:])
		if err != nil {
			return nil, err
		}
		if left.Kind != ExprModifier {
			left = &Expr{Kind: ExprModifier, Left: left}
		}
		left.Modifiers = append(left.Modifiers, TypedModifier{Include: token.value[0] == '+', Selector: selector})
	}
}

func (parser *expressionParser) chain() (*Expr, error) {
	left, err := parser.primary()
	if err != nil {
		return nil, err
	}
	left, err = parser.modifiers(left)
	if err != nil {
		return nil, err
	}
	for {
		token, ok := parser.take("relation")
		if !ok {
			return left, nil
		}
		step := &Expr{Kind: ExprRelation, Relation: token.value, Left: left}
		if strings.HasPrefix(token.value, "<<") {
			depth, err := relationDepth(token.value, 3)
			if err != nil {
				return nil, err
			}
			step.Depth = depth
		}
		for {
			filter, ok := parser.take("filter")
			if !ok {
				break
			}
			value := ""
			if filter.value != "~w" {
				arg, found := parser.take("value")
				if !found {
					return nil, fmt.Errorf("%s requires a value", filter.value)
				}
				value = arg.value
			}
			step.Filters = append(step.Filters, Filter{Kind: filter.value, Value: value})
		}
		if parser.index < len(parser.tokens) {
			switch parser.tokens[parser.index].kind {
			case "selector", "symbol", "open":
				if token.value == "=" {
					return nil, errors.New("definition relation has no right operand")
				}
				right, err := parser.primary()
				if err != nil {
					return nil, err
				}
				step.Right, err = parser.modifiers(right)
				if err != nil {
					return nil, err
				}
			}
		}
		left = step
	}
}

func relationDepth(value string, fallback int) (int, error) {
	digits := strings.TrimLeft(value, "<>")
	if digits == "" {
		return fallback, nil
	}
	depth, err := strconv.Atoi(digits)
	if err != nil || depth < 1 || depth > 8 {
		return 0, fmt.Errorf("%s depth must be between 1 and 8", value)
	}
	return depth, nil
}

func validateSymbolPattern(value string) error {
	if strings.Contains(value, "..") || strings.HasSuffix(value, ".") || strings.Count(value, "*") > 1 || (strings.Contains(value, "*") && !strings.HasSuffix(value, ".*")) {
		return fmt.Errorf("invalid Go symbol %q", value)
	}
	return nil
}

// Parse applies the generated PEG grammar and builds a compact expression tree.
func Parse(input string) (Query, error) {
	if strings.TrimSpace(input) == "" {
		return Query{}, errors.New("UIR query is required")
	}
	parser := &queryGrammar{Buffer: input}
	if err := parser.Init(); err != nil {
		return Query{}, fmt.Errorf("initialize UIR query parser: %w", err)
	}
	if err := parser.Parse(); err != nil {
		return Query{}, fmt.Errorf("parse compact UIR query %q: %w", input, err)
	}
	expression, err := (&expressionParser{tokens: parser.tokens}).expression()
	if err != nil {
		return Query{}, fmt.Errorf("parse compact UIR query %q: %w", input, err)
	}
	return Query{Expr: expression}, nil
}
