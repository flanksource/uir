package schemagen

import (
	"regexp"
	"strings"

	"github.com/flanksource/uir"
)

func refinementSchema(kind string) *Schema {
	hierarchy := &Schema{Type: "string", AnyOf: []*Schema{
		{Type: "string", Const: kind},
		{Type: "string", Pattern: "^" + regexp.QuoteMeta(kind) + ":"},
	}}
	var excluded []*Schema
	for _, other := range uir.StatementMarshaler.Kinds() {
		if strings.HasPrefix(other, kind+":") {
			excluded = append(excluded,
				&Schema{Type: "string", Const: other},
				&Schema{Type: "string", Pattern: "^" + regexp.QuoteMeta(other) + ":"},
			)
		}
	}
	if len(excluded) > 0 {
		hierarchy.Not = &Schema{AnyOf: excluded}
	}
	cross := uir.StatementMarshaler.CrossHierarchy()[kind]
	if len(cross) == 0 {
		return hierarchy
	}
	return &Schema{Type: "string", AnyOf: []*Schema{hierarchy, {Type: "string", Enum: cross}}}
}
