package uir

import (
	"fmt"
	"reflect"
	"slices"
)

// StatementScope is where in a body WalkStatements reached a statement.
type StatementScope struct {
	// Guards are the conditions that must hold to reach the statement, outermost
	// first. An else branch carries the negated condition, a switch case the
	// match against the switch's value, and its default the negated disjunction
	// of the other cases.
	Guards []ConditionStmt
	// Loops is the number of loops enclosing the statement.
	Loops int
	// Order is the statement's position in the walk, counted from zero.
	Order int
}

// WalkStatements visits stmts and every statement nested in them, depth-first,
// passing each in its value form (T, never *T) with the scope it was reached in.
// visit returning false skips that statement's children, not the rest of the
// walk. A condition is walked in the scope of its own statement, so a call
// inside an if condition is not guarded by that condition; a for loop's update
// is walked in the scope of its body. A nil statement, and a statement type the
// walker has no case for, panic.
func WalkStatements(stmts []Statement, visit func(Statement, StatementScope) bool) {
	w := &statementWalker{visit: visit}
	w.all(stmts, StatementScope{})
}

type statementWalker struct {
	visit func(Statement, StatementScope) bool
	order int
}

func (w *statementWalker) all(stmts []Statement, scope StatementScope) {
	for _, stmt := range stmts {
		w.walk(stmt, scope)
	}
}

func (w *statementWalker) walk(stmt Statement, scope StatementScope) {
	stmt = statementValue(stmt)
	scope.Order = w.order
	w.order++
	if !w.visit(stmt, scope) {
		return
	}
	if !w.control(stmt, scope) && !w.expression(stmt, scope) && !w.call(stmt, scope) && !isLeafStatement(stmt) {
		panic(fmt.Sprintf("uir: WalkStatements does not know statement type %T", stmt))
	}
}

// statementValue returns the value form of a statement held by pointer, so the
// walker needs one case per type.
func statementValue(stmt Statement) Statement {
	if stmt == nil {
		panic("uir: WalkStatements reached a nil statement")
	}
	v := reflect.ValueOf(stmt)
	if v.Kind() != reflect.Pointer {
		return stmt
	}
	if v.IsNil() {
		panic(fmt.Sprintf("uir: WalkStatements reached a nil %T", stmt))
	}
	value, ok := v.Elem().Interface().(Statement)
	if !ok {
		panic(fmt.Sprintf("uir: WalkStatements does not know statement type %T", stmt))
	}
	return value
}

func (w *statementWalker) control(stmt Statement, scope StatementScope) bool {
	switch s := stmt.(type) {
	case BlockStmt:
		w.all(s.Children, scope)
	case TestStmt:
		w.all(s.Children, scope)
	case IfStmt:
		w.walk(s.Condition, scope)
		w.walk(s.Then, scope.guardedBy(s.Condition))
		if s.Else != nil {
			w.walk(*s.Else, scope.guardedBy(negated(s.Condition)))
		}
	case SwitchStmt:
		w.switchCases(s, scope)
	case ForStmt:
		body := scope.looping(s.Cond)
		w.walk(s.Init, scope)
		w.walk(s.Cond, scope)
		w.walk(s.Body, body)
		w.walk(s.Update, body)
	case ForEachStmt:
		w.walk(s.Variable, scope)
		w.walk(s.Iterable, scope)
		w.walk(s.Body, scope.looping(ConditionStmt{}))
	case WhileStmt:
		w.walk(s.Condition, scope)
		w.walk(s.Body, scope.looping(s.Condition))
	case TryStmt:
		w.walk(s.Body, scope)
		w.walk(s.Catch, scope)
	case ReturnStmt:
		w.walk(s.Value, scope)
	case ThrowStmt:
		w.walk(s.Exception, scope)
	case ConditionStmt:
		w.walk(s.Expr, scope)
	case FunctionDeclStmt:
		if s.Body != nil {
			w.walk(*s.Body, scope)
		}
	default:
		return false
	}
	return true
}

// switchCases guards each case body by the case's match and the default body by
// the negation of every other case's match. A switch with only a default runs
// it unconditionally.
func (w *statementWalker) switchCases(s SwitchStmt, scope StatementScope) {
	w.walk(s.Value, scope)
	var matches []ExprStmt
	for _, c := range s.Cases {
		if !emptyCondition(c.Condition) {
			matches = append(matches, caseMatch(s.Value, c.Condition))
		}
	}
	for _, c := range s.Cases {
		w.walk(c.Condition, scope)
		body := scope
		switch {
		case !emptyCondition(c.Condition):
			body = scope.guardedBy(NewCondition(caseMatch(s.Value, c.Condition)))
		case len(matches) > 0:
			body = scope.guardedBy(negated(NewCondition(anyOf(matches))))
		}
		w.walk(c.Body, body)
	}
}

func (w *statementWalker) expression(stmt Statement, scope StatementScope) bool {
	switch s := stmt.(type) {
	case ExprStmt:
		if value := s.Value(); value != nil {
			w.walk(value, scope)
		}
	case AssignmentStmt:
		w.walk(s.Value, scope)
	case BinaryStmt:
		w.walk(s.Left, scope)
		w.walk(s.Right, scope)
	case UnaryStmt:
		w.walk(s.Operand, scope)
	case CastStmt:
		w.walk(s.Expr, scope)
	case TupleStmt:
		for _, element := range s.Elements {
			w.walk(element, scope)
		}
	case ObjectLiteralStmt:
		for _, entry := range s.Entries {
			w.walk(entry.Value, scope)
		}
	case TemplateLiteralStmt:
		for _, expr := range s.Expressions {
			w.walk(expr, scope)
		}
		if s.Tag != nil {
			w.walk(*s.Tag, scope)
		}
	case DestructuringStmt:
		w.walk(s.Value, scope)
		for _, binding := range s.Bindings {
			if binding.Default != nil {
				w.walk(*binding.Default, scope)
			}
			if binding.Nested != nil {
				w.walk(*binding.Nested, scope)
			}
		}
	default:
		return false
	}
	return true
}

func (w *statementWalker) call(stmt Statement, scope StatementScope) bool {
	switch s := stmt.(type) {
	case MethodCallStmt:
		w.receiver(s.Receiver, scope)
		w.arguments(s.Arguments, scope)
	case DispatchCallStmt:
		w.receiver(s.Receiver, scope)
		w.arguments(s.Arguments, scope)
	case EndpointCallStmt:
		w.arguments(s.Arguments, scope)
	case RecordReadStmt:
		w.arguments(s.Arguments, scope)
	case RecordWriteStmt:
		w.arguments(s.Arguments, scope)
	default:
		return false
	}
	return true
}

func (w *statementWalker) receiver(receiver *ExprStmt, scope StatementScope) {
	if receiver != nil {
		w.walk(*receiver, scope)
	}
}

func (w *statementWalker) arguments(arguments Arguments, scope StatementScope) {
	for _, argument := range arguments {
		w.walk(argument.Value, scope)
	}
}

func isLeafStatement(stmt Statement) bool {
	switch stmt.(type) {
	case LiteralStmt, VariableStmt, ScopedVariableRef, BreakStmt, ContinueStmt,
		VariableDeclStmt, ConstDeclStmt, TypeDeclStmt, RawStmt, DocStmt:
		return true
	}
	return false
}

// guardedBy copies the guards before adding to them, so sibling scopes never
// share a backing array.
func (s StatementScope) guardedBy(condition ConditionStmt) StatementScope {
	s.Guards = append(slices.Clip(s.Guards), condition)
	return s
}

// looping enters a loop body, guarded by the loop's condition when it has one.
func (s StatementScope) looping(condition ConditionStmt) StatementScope {
	s.Loops++
	if emptyCondition(condition) {
		return s
	}
	return s.guardedBy(condition)
}

// emptyCondition reports a condition that says nothing: its expression has no
// variant and no source text. A switch case with one is the default, and a loop
// with one has no condition to guard its body by.
func emptyCondition(condition ConditionStmt) bool {
	return emptyExpr(condition.Expr)
}

func emptyExpr(expr ExprStmt) bool {
	return expr.Value() == nil && (expr.Content == nil || *expr.Content == "")
}

func negated(condition ConditionStmt) ConditionStmt {
	return NewCondition(UnaryExpr(UnaryOpNot, condition.Expr))
}

// caseMatch is what must hold for a switch case to run: its condition, compared
// to the switch's value when the switch has one.
func caseMatch(value ExprStmt, condition ConditionStmt) ExprStmt {
	if emptyExpr(value) {
		return condition.Expr
	}
	return BinaryExpr(value, BinaryOpEqual, condition.Expr)
}

func anyOf(exprs []ExprStmt) ExprStmt {
	either := exprs[0]
	for _, expr := range exprs[1:] {
		either = BinaryExpr(either, BinaryOpOr, expr)
	}
	return either
}
