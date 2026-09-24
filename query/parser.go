package query

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const grammarUsage = `expected "nodes [where field = \"value\"]", "references|definitions|implementations|callees of node where ...", "callers of node where ... [including dispatch]", "search \"prefix\"", or "unresolved calls"; predicate values must be quoted`

type parserState struct {
	result       Query
	currentField string
	currentValue string
	err          error
}

func (state *parserState) setOperation(operation Operation) {
	state.result.Operation = operation
}

func (state *parserState) setField(field string) {
	state.currentField = field
}

func (state *parserState) setValue(raw string) {
	value, err := strconv.Unquote(`"` + raw + `"`)
	if err != nil {
		state.err = fmt.Errorf("decode quoted query value: %w", err)
		return
	}
	state.currentValue = value
}

func (state *parserState) setDispatch() {
	state.result.Dispatch = true
}

func (state *parserState) setSearch() {
	state.result.Search = state.currentValue
}

func (state *parserState) addPredicate() {
	if state.err != nil {
		return
	}
	state.result.Predicates = append(state.result.Predicates, Predicate{
		Field: state.currentField,
		Value: state.currentValue,
	})
}

// Parse applies the generated PEG grammar to one complete query expression.
func Parse(input string) (Query, error) {
	if strings.TrimSpace(input) == "" {
		return Query{}, errors.New("UIR query is required")
	}
	parser := &queryGrammar{Buffer: input}
	if err := parser.Init(); err != nil {
		return Query{}, fmt.Errorf("initialize UIR query parser: %w", err)
	}
	if err := parser.Parse(); err != nil {
		return Query{}, fmt.Errorf("parse UIR query %q: %s: %w", input, grammarUsage, err)
	}
	if parser.err != nil {
		return Query{}, fmt.Errorf("parse UIR query %q: %w", input, parser.err)
	}
	return parser.result, nil
}
