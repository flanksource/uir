package uir

import (
	"fmt"

	"github.com/flanksource/clicky/api"
)

// Pretty parenthesises a binary operand: !(a == b) is not !a == b.
func (s UnaryStmt) Pretty() api.Text {
	operand := s.Operand.Pretty()
	if s.Operand.Binary != nil {
		operand = operand.Wrap("(", ")", StylePunctuation)
	}
	return api.Text{Content: string(s.Operator), Style: StyleOperator}.Add(operand)
}

func (s UnaryStmt) String() string {
	return s.Pretty().ANSI()
}

// Pretty prints the value alone: its type shows as the literal family's style.
func (s LiteralStmt) Pretty() api.Text {
	value := ""
	if s.Value != nil {
		value = *s.Value
	}
	if s.FieldType != RecordFieldTypeNumber &&
		s.FieldType != RecordFieldTypeFloat &&
		s.FieldType != RecordFieldTypeInt &&
		s.FieldType != RecordFieldTypeBoolean {
		value = fmt.Sprintf("\"%s\"", value)
	}
	return api.Text{Content: value, Style: LiteralStyle(s.FieldType)}
}

func (s LiteralStmt) String() string {
	return s.Pretty().ANSI()
}

// Pretty parenthesises a binary operand wherever leaving the parentheses out
// would read as a different expression: a && (b || c) is not a && b || c.
func (s BinaryStmt) Pretty() api.Text {
	return s.operand(s.Left, false).Append(" "+string(s.Operator)+" ", StyleBinaryOperator).Add(s.operand(s.Right, true))
}

func (s BinaryStmt) String() string {
	return s.Pretty().ANSI()
}

func (s BinaryStmt) operand(expr ExprStmt, right bool) api.Text {
	text := expr.Pretty()
	if expr.Binary != nil && binaryOperandNeedsParens(s.Operator, expr.Binary.Operator, right) {
		return text.Wrap("(", ")", StylePunctuation)
	}
	return text
}

// binaryPrecedence ranks the operators every language ranks alike, tightest
// highest. The bitwise, shift and nullish operators are left out on purpose: Go
// binds & like *, C binds it looser than ==, and ?? may not sit beside || at
// all, so an operand on either side of one is always parenthesised.
var binaryPrecedence = map[BinaryOp]int{
	BinaryOpOr:             1,
	BinaryOpAnd:            2,
	BinaryOpEqual:          3,
	BinaryOpNotEqual:       3,
	BinaryOpStrictEqual:    3,
	BinaryOpStrictNotEqual: 3,
	BinaryOpLess:           3,
	BinaryOpLessEqual:      3,
	BinaryOpGreater:        3,
	BinaryOpGreaterEqual:   3,
	BinaryOpLike:           3,
	BinaryOpILike:          3,
	BinaryOpRegex:          3,
	BinaryOpStartsWith:     3,
	BinaryOpEndsWith:       3,
	BinaryOpContains:       3,
	BinaryOpIn:             3,
	BinaryOpInstanceOf:     3,
	BinaryOpAdd:            4,
	BinaryOpSubtract:       4,
	BinaryOpMultiply:       5,
	BinaryOpDivide:         5,
	BinaryOpModulus:        5,
	BinaryOpExponent:       6,
	BinaryOpOptionalChain:  7,
}

// binaryOperandNeedsParens reports whether a binary operand of parent, itself
// built with child, must be parenthesised. Operators group to the left, except
// ** which groups to the right; a chain of && or of || needs none either way.
func binaryOperandNeedsParens(parent, child BinaryOp, right bool) bool {
	parentRank, childRank := binaryPrecedence[parent], binaryPrecedence[child]
	if parentRank == 0 || childRank == 0 {
		return parent != child || right
	}
	if parentRank != childRank {
		return childRank < parentRank
	}
	if parent == child && (parent == BinaryOpAnd || parent == BinaryOpOr) {
		return false
	}
	return right != (parent == BinaryOpExponent)
}
