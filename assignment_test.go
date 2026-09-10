package uir

import (
	"strings"
	"testing"
)

func TestAssignmentOpAppend_InAllAssignmentOps(t *testing.T) {
	for _, op := range AllAssignmentOps {
		if op == AssignmentOpAppend {
			return
		}
	}
	t.Errorf("AssignmentOpAppend missing from AllAssignmentOps")
}

func TestAssignmentOpAppend_PrettyRendersAsAngleAngle(t *testing.T) {
	stmt := NewAssignment(
		"this.validations",
		ExprStmt{Literal: ptr(Literal("validation", RecordFieldTypeString))},
	).WithOp(AssignmentOpAppend).Build()

	got := stmt.Pretty().String()
	if !strings.Contains(got, " << ") {
		t.Errorf("AssignmentStmt.Pretty() with append op should render `target << value`, got: %q", got)
	}
	if strings.HasPrefix(got, ".") {
		t.Errorf("AssignmentStmt.Pretty() should not start with a leading `.`, got: %q", got)
	}
}

func ptr[T any](value T) *T {
	return &value
}
