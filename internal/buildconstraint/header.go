package buildconstraint

import (
	"bytes"
	"context"
	"fmt"
	"go/build/constraint"
	"go/scanner"
	"go/token"
	"strings"
)

// Header is the modern build expression of a source's leading comment header.
type Header struct {
	expression constraint.Expr
}

func Parse(content []byte) (Header, error) {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	file := token.NewFileSet().AddFile("", -1, len(content))
	var lexer scanner.Scanner
	var scanError error
	lexer.Init(file, content, func(pos token.Position, message string) {
		if scanError == nil {
			scanError = fmt.Errorf("header line %d: %s", pos.Line, message)
		}
	}, scanner.ScanComments)
	var header Header
	for {
		position, kind, text := lexer.Scan()
		if scanError != nil {
			return Header{}, scanError
		}
		if kind != token.COMMENT {
			return header, nil
		}
		offset := file.Offset(position)
		lineStart := bytes.LastIndexByte(content[:offset], '\n') + 1
		if len(bytes.TrimSpace(content[lineStart:offset])) != 0 || !constraint.IsGoBuild(text) {
			continue
		}
		if header.expression != nil {
			return Header{}, fmt.Errorf("header line %d: multiple //go:build directives", file.Line(position))
		}
		expression, err := constraint.Parse(strings.Clone(text))
		if err != nil {
			return Header{}, fmt.Errorf("header line %d: %w", file.Line(position), err)
		}
		header.expression = expression
	}
}

func (header Header) String() string {
	if header.expression == nil {
		return ""
	}
	return header.expression.String()
}

func (header Header) Requires(ctx context.Context, tags []string) (map[string]bool, error) {
	for _, tag := range tags {
		if err := ValidateTag(tag); err != nil {
			return nil, err
		}
	}
	diagram := newDiagram(ctx)
	root, err := diagram.compile(header.expression)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result[tag] = root != 0 && !diagram.without(root, tag, map[int]bool{})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func ValidateTag(tag string) error {
	expression, err := constraint.Parse("//go:build " + tag)
	if parsed, ok := expression.(*constraint.TagExpr); err == nil && ok && parsed.Tag == tag {
		return nil
	}
	return fmt.Errorf("REQUIRES expects one positive Go build tag, got %q", tag)
}
