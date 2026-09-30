package uir_test

import (
	"fmt"
	"reflect"

	"github.com/flanksource/clicky/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

func callTo(name string) uir.MethodCallStmt {
	return uir.NewMethodCall(name).Build()
}

func callExprTo(name string) uir.ExprStmt {
	call := callTo(name)
	return uir.ExprStmt{MethodCall: &call}
}

func blockOf(stmts ...uir.Statement) uir.BlockStmt {
	return uir.NewBlock().WithStatements(stmts...).Build()
}

func ifThen(condition uir.ExprStmt, then ...uir.Statement) *uir.IfBuilder {
	return uir.NewIf(condition).WithThen(blockOf(then...))
}

func guardText(guards []uir.ConditionStmt) []string {
	text := []string{}
	for _, guard := range guards {
		text = append(text, guard.Pretty().String())
	}
	return text
}

// callScopes walks stmts and returns the scope each called method was reached in.
func callScopes(stmts ...uir.Statement) map[string]uir.StatementScope {
	scopes := map[string]uir.StatementScope{}
	uir.WalkStatements(stmts, func(stmt uir.Statement, scope uir.StatementScope) bool {
		if call, ok := stmt.(uir.MethodCallStmt); ok {
			scopes[call.Method.GetIdentifier().Method] = scope
		}
		return true
	})
	return scopes
}

func callGuards(stmts ...uir.Statement) map[string][]string {
	guards := map[string][]string{}
	for name, scope := range callScopes(stmts...) {
		guards[name] = guardText(scope.Guards)
	}
	return guards
}

type unregisteredStmt struct{}

func (unregisteredStmt) GetStatementType() uir.StatementType { return "unregistered" }
func (unregisteredStmt) Pretty() api.Text                    { return api.Text{} }
func (unregisteredStmt) String() string                      { return "unregistered" }
func (unregisteredStmt) GetSignature() string                { return "unregistered" }

func registeredStatementEntries() []TableEntry {
	var entries []TableEntry
	for _, prototype := range uir.Statements {
		t := reflect.TypeOf(prototype)
		entries = append(entries,
			Entry(fmt.Sprintf("%s as value", t), reflect.Zero(t).Interface().(uir.Statement), t),
			Entry(fmt.Sprintf("%s as pointer", t), reflect.New(t).Interface().(uir.Statement), t),
		)
	}
	return entries
}

// childPositionEntries puts a call to target in each child field a statement
// can hold one in.
func childPositionEntries() []TableEntry {
	target := callExprTo("target")
	other := uir.VarExpr("other")
	test := uir.NewTest("walks")
	test.Children = []uir.Statement{callTo("target")}
	inner := uir.NewMethod("inner").WithBody(blockOf(callTo("target"))).Build()

	return []TableEntry{
		Entry("a block child", blockOf(callTo("target"))),
		Entry("a test child", test),
		Entry("a switch value", uir.NewSwitch(target).Build()),
		Entry("a switch case condition", uir.NewSwitch(other).WithCase(uir.NewCondition(target), blockOf()).Build()),
		Entry("a switch case body", uir.NewSwitch(other).WithCase(uir.NewCondition(other), blockOf(callTo("target"))).Build()),
		Entry("a for init", uir.NewFor().WithInit(uir.NewAssignment("i", target).Build()).Build()),
		Entry("a for condition", uir.NewFor().WithCondition(uir.NewCondition(target)).Build()),
		Entry("a for body", uir.NewFor().WithBody(blockOf(callTo("target"))).Build()),
		Entry("a for update", uir.NewFor().WithUpdate(target).Build()),
		Entry("a for-each variable", uir.NewForEach().WithVariable(uir.NewAssignment("item", target).Build()).Build()),
		Entry("a for-each iterable", uir.NewForEach().WithIterable(target).Build()),
		Entry("a for-each body", uir.NewForEach().WithBody(blockOf(callTo("target"))).Build()),
		Entry("a throw", uir.NewThrow(target)),
		Entry("an assignment value", uir.NewAssignment("x", target).Build()),
		Entry("a binary left operand", uir.BinaryExpr(target, uir.BinaryOpAnd, other)),
		Entry("a binary right operand", uir.BinaryExpr(other, uir.BinaryOpAnd, target)),
		Entry("a unary operand", uir.UnaryExpr(uir.UnaryOpNot, target)),
		Entry("a cast", uir.CastExpr(target, "Order")),
		Entry("a tuple element", uir.TupleStmt{Elements: []uir.ExprStmt{other, target}}),
		Entry("an object literal entry", uir.ObjectExpr(uir.ObjectEntry("order", target))),
		Entry("a template expression", uir.TemplateLiteralStmt{Expressions: []uir.ExprStmt{target}}),
		Entry("a template tag", uir.TemplateLiteralStmt{Tag: &target}),
		Entry("a destructuring default", uir.DestructuringStmt{Bindings: []uir.DestructureBinding{{Name: "id", Default: &target}}}),
		Entry("a nested destructuring", uir.DestructuringStmt{Bindings: []uir.DestructureBinding{{Name: "id", Nested: &uir.DestructuringStmt{Value: target}}}}),
		Entry("a dispatch call receiver", uir.NewDispatchCall("run").WithReceiver(target).Build()),
		Entry("a dispatch call argument", uir.NewDispatchCall("run").WithArgument("order", target).Build()),
		Entry("an endpoint call argument", uir.NewEndpointCall(uir.EndpointTypeHTTP, nil).WithPositionalArg(target).Build()),
		Entry("a record read argument", uir.NewRecordRead(uir.RecordTypeTable, nil).WithArgument("id", target).Build()),
		Entry("a record write argument", uir.NewRecordWrite(uir.RecordTypeTable, nil).WithArgument("id", target).Build()),
		Entry("a nested function's body", uir.FunctionDeclStmt{MethodNode: inner}),
	}
}

var _ = Describe("WalkStatements", func() {
	var (
		a, b, c, d, e = uir.VarExpr("a"), uir.VarExpr("b"), uir.VarExpr("c"), uir.VarExpr("d"), uir.VarExpr("e")
		status        = uir.VarExpr("status")
		active        = uir.VarExpr("ACTIVE")
		closed        = uir.VarExpr("CLOSED")
		xOverY        = uir.BinaryExpr(uir.VarExpr("x"), uir.BinaryOpGreater, uir.VarExpr("y"))
		iBelowN       = uir.BinaryExpr(uir.VarExpr("i"), uir.BinaryOpLess, uir.VarExpr("n"))
	)

	DescribeTable("visits the value form of every registered statement",
		func(stmt uir.Statement, want reflect.Type) {
			var visited []reflect.Type
			uir.WalkStatements([]uir.Statement{stmt}, func(s uir.Statement, _ uir.StatementScope) bool {
				visited = append(visited, reflect.TypeOf(s))
				return true
			})
			Expect(visited).NotTo(BeEmpty())
			Expect(visited[0]).To(Equal(want))
		},
		registeredStatementEntries(),
	)

	It("panics naming a statement type it does not know", func() {
		Expect(func() {
			uir.WalkStatements([]uir.Statement{unregisteredStmt{}}, func(uir.Statement, uir.StatementScope) bool { return true })
		}).To(PanicWith(ContainSubstring("uir_test.unregisteredStmt")))
	})

	It("panics naming the type of a nil statement pointer", func() {
		var missing *uir.IfStmt
		Expect(func() {
			uir.WalkStatements([]uir.Statement{missing}, func(uir.Statement, uir.StatementScope) bool { return true })
		}).To(PanicWith(ContainSubstring("*uir.IfStmt")))
	})

	DescribeTable("gives each call the conditions that must hold to reach it, outermost first",
		func(stmt uir.Statement, want map[string][]string) {
			Expect(callGuards(stmt)).To(Equal(want))
		},
		Entry("under if and else",
			ifThen(a, callTo("then")).WithElse(blockOf(callTo("otherwise"))).Build(),
			map[string][]string{"then": {"a"}, "otherwise": {"!a"}},
		),
		Entry("under else if",
			ifThen(a, callTo("first")).WithElse(blockOf(
				ifThen(b, callTo("second")).WithElse(blockOf(callTo("neither"))).Build(),
			)).Build(),
			map[string][]string{"first": {"a"}, "second": {"!a", "b"}, "neither": {"!a", "!b"}},
		),
		Entry("under the negation of a binary condition",
			ifThen(xOverY, callTo("then")).WithElse(blockOf(callTo("otherwise"))).Build(),
			map[string][]string{"then": {"x > y"}, "otherwise": {"!(x > y)"}},
		),
		Entry("under an if nested in a for",
			uir.NewFor().WithCondition(uir.NewCondition(iBelowN)).WithBody(blockOf(
				ifThen(a, callTo("inner")).Build(),
				callTo("each"),
			)).Build(),
			map[string][]string{"inner": {"i < n", "a"}, "each": {"i < n"}},
		),
		Entry("under a while",
			uir.NewWhile(uir.NewCondition(a)).WithBody(blockOf(callTo("poll"))).Build(),
			map[string][]string{"poll": {"a"}},
		),
		Entry("under the cases and the default of a switch over a value",
			uir.NewSwitch(status).
				WithCase(uir.NewCondition(active), blockOf(callTo("activate"))).
				WithCase(uir.NewCondition(closed), blockOf(callTo("archive"))).
				WithCase(uir.ConditionStmt{}, blockOf(callTo("ignore"))).
				Build(),
			map[string][]string{
				"activate": {"status == ACTIVE"},
				"archive":  {"status == CLOSED"},
				"ignore":   {"!(status == ACTIVE || status == CLOSED)"},
			},
		),
		Entry("under the cases and the default of a tagless switch",
			uir.NewSwitch(uir.ExprStmt{}).
				WithCase(uir.NewCondition(xOverY), blockOf(callTo("over"))).
				WithCase(uir.NewCondition(a), blockOf(callTo("flagged"))).
				WithCase(uir.ConditionStmt{}, blockOf(callTo("neither"))).
				Build(),
			map[string][]string{
				"over":    {"x > y"},
				"flagged": {"a"},
				"neither": {"!(x > y || a)"},
			},
		),
		Entry("under a switch with only a default, which always runs",
			uir.NewSwitch(status).WithCase(uir.ConditionStmt{}, blockOf(callTo("always"))).Build(),
			map[string][]string{"always": {}},
		),
		Entry("in a return value",
			ifThen(a, uir.NewReturn(callExprTo("compute"))).Build(),
			map[string][]string{"compute": {"a"}},
		),
		Entry("in a call argument",
			ifThen(a, uir.NewMethodCall("outer").WithPositionalArg(callExprTo("inner")).Build()).Build(),
			map[string][]string{"outer": {"a"}, "inner": {"a"}},
		),
		Entry("in a receiver",
			ifThen(a, uir.NewMethodCall("method").WithReceiver(callExprTo("receiver")).Build()).Build(),
			map[string][]string{"method": {"a"}, "receiver": {"a"}},
		),
		Entry("in a destructuring value",
			ifThen(a, uir.DestructuringStmt{Bindings: []uir.DestructureBinding{{Name: "id"}}, Value: callExprTo("load")}).Build(),
			map[string][]string{"load": {"a"}},
		),
		Entry("in a try body and its catch, which add no condition",
			ifThen(a, uir.NewTry().WithBody(blockOf(callTo("attempt"))).WithCatch(blockOf(callTo("recover"))).Build()).Build(),
			map[string][]string{"attempt": {"a"}, "recover": {"a"}},
		),
		Entry("in an if condition, which does not guard itself",
			ifThen(callExprTo("check"), callTo("run")).Build(),
			map[string][]string{"check": {}, "run": {"check()"}},
		),
		Entry("in a loop condition, which does not guard itself",
			uir.NewWhile(uir.NewCondition(callExprTo("hasNext"))).WithBody(blockOf(callTo("next"))).Build(),
			map[string][]string{"hasNext": {}, "next": {"hasNext()"}},
		),
	)

	DescribeTable("reaches a call in every child position",
		func(stmt uir.Statement) {
			Expect(callScopes(stmt)).To(HaveKey("target"))
		},
		childPositionEntries(),
	)

	It("counts the loops enclosing each call, and pushes no guard for a loop without a condition", func() {
		scopes := callScopes(
			callTo("once"),
			uir.NewForEach().WithIterable(uir.VarExpr("items")).WithBody(blockOf(
				callTo("each"),
				uir.NewWhile(uir.ConditionStmt{}).WithBody(blockOf(callTo("forever"))).Build(),
				uir.NewFor().WithBody(blockOf(callTo("spin"))).Build(),
			)).Build(),
		)

		loops := map[string]int{}
		guards := map[string][]string{}
		for name, scope := range scopes {
			loops[name] = scope.Loops
			guards[name] = guardText(scope.Guards)
		}
		Expect(loops).To(Equal(map[string]int{"once": 0, "each": 1, "forever": 2, "spin": 2}))
		Expect(guards).To(Equal(map[string][]string{"once": {}, "each": {}, "forever": {}, "spin": {}}))
	})

	It("skips the children of a statement visit declines, and keeps walking its siblings", func() {
		var visited []string
		uir.WalkStatements(
			[]uir.Statement{
				ifThen(a, callTo("skipped")).Build(),
				callTo("sibling"),
			},
			func(stmt uir.Statement, _ uir.StatementScope) bool {
				switch s := stmt.(type) {
				case uir.IfStmt:
					return false
				case uir.MethodCallStmt:
					visited = append(visited, s.Method.GetIdentifier().Method)
				}
				return true
			},
		)
		Expect(visited).To(Equal([]string{"sibling"}))
	})

	It("numbers every visit in order, starting at zero", func() {
		var orders []int
		uir.WalkStatements(
			[]uir.Statement{ifThen(a, callTo("first")).Build(), callTo("second")},
			func(_ uir.Statement, scope uir.StatementScope) bool {
				orders = append(orders, scope.Order)
				return true
			},
		)
		want := make([]int, len(orders))
		for i := range want {
			want[i] = i
		}
		Expect(orders).To(HaveLen(7), "if, condition, expr, variable, then block, call, then the sibling call")
		Expect(orders).To(Equal(want))
	})

	It("gives sibling scopes guard slices that do not alias", func() {
		scopes := callScopes(
			ifThen(a, ifThen(b, ifThen(c,
				ifThen(d, callTo("left")).Build(),
				ifThen(e, callTo("right")).Build(),
			).Build()).Build()).Build(),
		)
		Expect(map[string][]string{
			"left":  guardText(scopes["left"].Guards),
			"right": guardText(scopes["right"].Guards),
		}).To(Equal(map[string][]string{
			"left":  {"a", "b", "c", "d"},
			"right": {"a", "b", "c", "e"},
		}))
	})
})
