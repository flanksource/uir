package graph

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir"
)

// accessTypes are the edge types a request names to follow, in the order
// ParseAccess returns them.
var accessTypes = []uir.RelationshipType{uir.RelationshipTypeCall, uir.RelationshipTypeRead, uir.RelationshipTypeWrite}

// relationshipTypes are the uir.RelationshipType values an edge can have,
// which Options.Access may list.
var relationshipTypes = []uir.RelationshipType{
	uir.RelationshipTypeImport, uir.RelationshipTypeCall, uir.RelationshipTypeReference, uir.RelationshipTypeInheritance,
	uir.RelationshipTypeImplements, uir.RelationshipTypeIncludes, uir.RelationshipTypeForeignKey,
	uir.RelationshipTypeRead, uir.RelationshipTypeWrite, uir.RelationshipTypeDispatch,
}

// ParseAccess reads the edge types a request asks to follow, as a flag or a
// query parameter gives them: call, read and write, in any case, each value
// possibly a comma-separated list. It returns each type once, in call, read,
// write order, and nil, which Options.Access reads as every type, when the
// values name none.
func ParseAccess(values []string) ([]uir.RelationshipType, error) {
	held := map[uir.RelationshipType]bool{}
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if !slices.Contains(accessTypes, uir.RelationshipType(part)) {
				return nil, fmt.Errorf("graph: access %q is not one of call, read, write", part)
			}
			held[uir.RelationshipType(part)] = true
		}
	}
	if len(held) == 0 {
		return nil, nil
	}
	return slices.DeleteFunc(slices.Clone(accessTypes), func(t uir.RelationshipType) bool { return !held[t] }), nil
}

// Follows reports whether an edge of type t is followed under access, as
// Options.Access lists it: an empty access follows every type, and call
// follows a dispatch too, since a dispatch is a call that reaches one
// implementation.
func Follows(access []uir.RelationshipType, t uir.RelationshipType) bool {
	if len(access) == 0 {
		return true
	}
	return slices.Contains(access, t) || (t == uir.RelationshipTypeDispatch && slices.Contains(access, uir.RelationshipTypeCall))
}

// validateAccess refuses an access entry that is no uir.RelationshipType.
func validateAccess(access []uir.RelationshipType) error {
	for _, t := range access {
		if !slices.Contains(relationshipTypes, t) {
			return fmt.Errorf("graph: access lists %q, which is not a relationship type", t)
		}
	}
	return nil
}
