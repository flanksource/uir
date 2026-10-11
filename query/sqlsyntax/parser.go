package sqlsyntax

import (
	"fmt"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

const selectorValue = `(?:"(?:\\.|[^"\\])*"|(?:\\.|[\pL\pN_./*?@#$!-])+)`

var sqlLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "Comment", Pattern: `--[^\n]*|/\*(?:[^*]|\*+[^*/])*\*+/`},
	{Name: "Whitespace", Pattern: `\s+`},
	{Name: "Selector", Pattern: `[\pL_][\pL\pN_.]*:` + selectorValue + `(?::` + selectorValue + `)?`},
	{Name: "String", Pattern: `'(?:''|[^'])*'`},
	{Name: "Number", Pattern: `[-+]?[0-9]+(?:\.[0-9]+)?`},
	{Name: "Ident", Pattern: `[\pL_][\pL\pN_.]*`},
	{Name: "Operator", Pattern: `=>|!=|<=|>=|[=<>*(),;]`},
})

var sqlParser = participle.MustBuild[Query](
	participle.Lexer(sqlLexer), participle.Elide("Whitespace", "Comment"),
	participle.CaseInsensitive("Ident"), participle.UseLookahead(4),
)

type SQLString string

func (value *SQLString) Capture(tokens []string) error {
	*value = SQLString(strings.ReplaceAll(tokens[0][1:len(tokens[0])-1], "''", "'"))
	return nil
}

type SQLBoolean bool

func (value *SQLBoolean) Capture(tokens []string) error {
	*value = SQLBoolean(strings.EqualFold(tokens[0], "TRUE"))
	return nil
}

func (literal Literal) Value() any {
	switch {
	case literal.String != nil:
		return string(*literal.String)
	case literal.Number != nil:
		return *literal.Number
	case literal.Boolean != nil:
		return bool(*literal.Boolean)
	case literal.Null:
		return nil
	default:
		panic("SQL literal has no value")
	}
}

func Parse(input string) (*Query, error) {
	if len(input) > 65536 {
		return nil, fmt.Errorf("SQL query exceeds 65536 bytes")
	}
	tokens, err := sqlLexer.LexString("", input)
	if err != nil {
		return nil, err
	}
	depth, negations := 0, 0
	for {
		token, err := tokens.Next()
		if err != nil {
			return nil, err
		}
		if token.EOF() {
			break
		}
		if strings.EqualFold(token.Value, "NOT") {
			negations++
		}
		if negations > 64 {
			return nil, participle.Errorf(token.Pos, "SQL negation exceeds 64 levels")
		}
		if token.Value == "(" {
			depth++
		}
		if token.Value == ")" {
			depth--
		}
		if depth > 64 {
			return nil, participle.Errorf(token.Pos, "SQL nesting exceeds 64 levels")
		}
	}
	return sqlParser.ParseString("", input)
}
