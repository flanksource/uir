package uir

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestPackageBuilder demonstrates building a complete package with types and functions
func TestPackageBuilder(t *testing.T) {
	pkg := NewPackage("com.example.api").
		WithModule("github.com/example/api").
		WithPath("src/main/api/server.go").
		WithVariable(Field("config", RecordFieldTypeObject)).
		WithVariable(Field("logger", RecordFieldTypeObject)).
		WithFunction(
			NewMethod("init").
				WithStatement(NewAssignment("config", LitExpr("default.yaml", RecordFieldTypeString)).Build()).
				Build(),
		).
		WithType(
			NewType("UserService").
				WithVariable(Field("db", RecordFieldTypeObject)).
				WithVariable(Field("cache", RecordFieldTypeObject)).
				WithMethod(
					NewMethod("GetUser").
						WithParam(Field("id", RecordFieldTypeString)).
						WithStatement(
							NewAssignment("user", ExprStmt{
								RecordRead: NewRecordRead(RecordTypeTable, NewRecord("users").Build()).WithExpression(ExpressionTypeSQL, "SELECT * FROM users WHERE id = ?").Build(),
							}).Build(),
						).
						WithStatement(
							NewReturn(VarExpr("user")),
						).
						WithReturn(Field("user", RecordFieldTypeObject)).
						Build(),
				).
				Build(),
		).
		Build()

	// Verify package structure
	if pkg.Package != "com.example.api" {
		t.Errorf("expected package name 'com.example.api', got %s", pkg.Package)
	}

	if len(pkg.Variables) != 2 {
		t.Errorf("expected 2 package variables, got %d", len(pkg.Variables))
	}

	if len(pkg.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(pkg.Functions))
	}

	if len(pkg.Types) != 1 {
		t.Errorf("expected 1 type, got %d", len(pkg.Types))
	}

	// Verify type structure
	userService := pkg.Types[0]
	if userService.Type != "UserService" {
		t.Errorf("expected type name 'UserService', got %s", userService.Type)
	}

	if len(userService.Variables) != 2 {
		t.Errorf("expected 2 fields in UserService, got %d", len(userService.Variables))
	}

	if len(userService.Methods) != 1 {
		t.Errorf("expected 1 method in UserService, got %d", len(userService.Methods))
	}

	// Verify method structure
	getUserMethod := userService.Methods[0]
	if getUserMethod.Method != "GetUser" {
		t.Errorf("expected method name 'GetUser', got %s", getUserMethod.Method)
	}

	if getUserMethod.Body == nil {
		t.Fatal("GetUser method should have a body")
	}

	if len(getUserMethod.Body.Children) != 2 {
		t.Errorf("expected 2 statements in GetUser body, got %d", len(getUserMethod.Body.Children))
	}

	// Marshal to JSON to verify structure
	_, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal package: %v", err)
	}

	fmt.Println(pkg.Pretty().ANSI())

}

// TestTypeBuilderImplementsAndExtends verifies the fluent helpers added for
// plugins that emit classes implementing external interfaces (e.g. PolicyAdmin roots
// implementing the TS contracts in plugins/policyadmin/base).
func TestTypeBuilderImplementsAndExtends(t *testing.T) {
	run := NewMethod("run").
		WithParam(Field("opts", RecordFieldTypeObject)).
		WithReturnType(SimpleType("PolicyEnrolmentResult")).
		Build()

	class := NewType("PolicyEnrolment").
		WithPackage("policyadmin.omrlac.GL").
		WithPath("file:///tmp/PolicyEnrolment.xml").
		WithImplements(GenericType("Transaction",
			SimpleType("PolicyEnrolmentOptions"),
			SimpleType("PolicyEnrolmentResult"),
		)).
		WithExtends(SimpleType("BaseTransaction")).
		WithTypeParams(TypeParam{Name: "T"}).
		WithMethod(run).
		Build()

	if got := class.Type; got != "PolicyEnrolment" {
		t.Fatalf("Type = %q, want %q", got, "PolicyEnrolment")
	}
	if len(class.Implements) != 1 {
		t.Fatalf("Implements len = %d, want 1", len(class.Implements))
	}
	impl := class.Implements[0]
	if impl.Name != "Transaction" {
		t.Fatalf("Implements[0].Name = %q, want %q", impl.Name, "Transaction")
	}
	if len(impl.TypeArgs) != 2 ||
		impl.TypeArgs[0].Name != "PolicyEnrolmentOptions" ||
		impl.TypeArgs[1].Name != "PolicyEnrolmentResult" {
		t.Fatalf("Implements[0].TypeArgs = %+v", impl.TypeArgs)
	}
	if class.Extends == nil || class.Extends.Name != "BaseTransaction" {
		t.Fatalf("Extends = %+v, want BaseTransaction", class.Extends)
	}
	if len(class.TypeParams) != 1 || class.TypeParams[0].Name != "T" {
		t.Fatalf("TypeParams = %+v", class.TypeParams)
	}
	if len(class.Methods) != 1 || class.Methods[0].Method != "run" {
		t.Fatalf("Methods = %+v", class.Methods)
	}
	if class.Methods[0].ReturnType == nil || class.Methods[0].ReturnType.Name != "PolicyEnrolmentResult" {
		t.Fatalf("Methods[0].ReturnType = %+v", class.Methods[0].ReturnType)
	}
}

// TestMethodBuilderWithControlFlow demonstrates building methods with control flow
func TestMethodBuilderWithControlFlow(t *testing.T) {
	method := NewMethod("ProcessOrder").
		WithType("OrderService").
		WithParam(Field("orderId", RecordFieldTypeString)).
		WithReturn(Field("result", RecordFieldTypeBoolean)).
		WithStatement(
			NewIf(BinaryExpr(
				VarExpr("orderId"),
				BinaryOpEqual,
				LitExpr("", RecordFieldTypeString),
			)).
				WithThen(
					NewBlock().
						WithStatement(NewReturn(LitExpr("false", RecordFieldTypeBoolean))).
						Build(),
				).
				Build(),
		).
		WithStatement(
			NewRecordRead(RecordTypeTable, NewRecord("users").Build()).
				WithArgument("id", VarExpr("orderId")).
				Build(),
		).
		WithStatement(
			NewMethodCall("validateOrder").
				WithReceiver(VarExpr("order")).
				Build(),
		).
		WithStatement(
			NewEndpointCall(EndpointTypeHTTP, NewEndpoint("http://payment.example.com/charge").Build()).
				WithPositionalArg(VarExpr("order")).
				Build(),
		).
		WithStatement(
			NewReturn(LitExpr("true", RecordFieldTypeBoolean)),
		).
		Build()

	// Verify method structure
	if method.Method != "ProcessOrder" {
		t.Errorf("expected method name 'ProcessOrder', got %s", method.Method)
	}

	if method.Type != "OrderService" {
		t.Errorf("expected type 'OrderService', got %s", method.Type)
	}

	if len(method.Params) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(method.Params))
	}

	if len(method.Returns) != 1 {
		t.Errorf("expected 1 return value, got %d", len(method.Returns))
	}

	if method.Body == nil {
		t.Fatal("method should have a body")
	}

	if len(method.Body.Children) != 5 {
		t.Errorf("expected 5 statements, got %d", len(method.Body.Children))
	}

	// Verify first statement is an if statement
	ifStmt, ok := method.Body.Children[0].(IfStmt)
	if !ok {
		t.Fatalf("expected first statement to be IfStmt, got %T", method.Body.Children[0])
	}

	if ifStmt.GetStatementType() != ASTStatementTypeIf {
		t.Errorf("expected IfStmt type, got %v", ifStmt.GetStatementType())
	}

	// Marshal to JSON
	_, err := json.MarshalIndent(method, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal method: %v", err)
	}

}

// TestLoopBuilders demonstrates building loops
func TestLoopBuilders(t *testing.T) {
	// For loop
	forLoop := NewFor().
		WithInit(NewAssignment("i", LitExpr("0", RecordFieldTypeNumber)).Build()).
		WithCondition(NewCondition(
			BinaryExpr(VarExpr("i"), BinaryOpLess, LitExpr("10", RecordFieldTypeNumber)),
		)).
		WithBody(
			NewBlock().
				WithStatement(NewMethodCall("process").WithPositionalArg(VarExpr("i")).Build()).
				Build(),
		).
		Build()

	if forLoop.GetStatementType() != ASTStatementTypeLoopFor {
		t.Errorf("expected ForStmt type, got %v", forLoop.GetStatementType())
	}

	// While loop
	whileLoop := NewWhile(NewCondition(VarExpr("hasMore"))).
		WithBody(
			NewBlock().
				WithStatement(NewMethodCall("fetchNext").Build()).
				Build(),
		).
		Build()

	if whileLoop.GetStatementType() != ASTStatementTypeLoopWhile {
		t.Errorf("expected WhileStmt type, got %v", whileLoop.GetStatementType())
	}

	// Marshal to verify structure
	forData, _ := json.Marshal(forLoop)
	whileData, _ := json.Marshal(whileLoop)

	t.Logf("For loop: %s", string(forData))
	t.Logf("While loop: %s", string(whileData))
}

// TestSwitchBuilder demonstrates building switch statements
func TestSwitchBuilder(t *testing.T) {
	switchStmt := NewSwitch(VarExpr("status")).
		WithCase(
			NewCondition(LitExpr("pending", RecordFieldTypeString)),
			NewBlock().WithStatement(NewMethodCall("handlePending").Build()).Build(),
		).
		WithCase(
			NewCondition(LitExpr("completed", RecordFieldTypeString)),
			NewBlock().WithStatement(NewMethodCall("handleCompleted").Build()).Build(),
		).
		Build()

	if switchStmt.GetStatementType() != ASTStatementTypeSwitch {
		t.Errorf("expected SwitchStmt type, got %v", switchStmt.GetStatementType())
	}

	if len(switchStmt.Cases) != 2 {
		t.Errorf("expected 2 cases, got %d", len(switchStmt.Cases))
	}

	data, _ := json.Marshal(switchStmt)
	t.Logf("Switch statement: %s", string(data))
}

// TestTryBuilder demonstrates building try-catch blocks
func TestTryBuilder(t *testing.T) {
	tryStmt := NewTry().
		WithBody(
			NewBlock().
				WithStatement(NewRecordWrite(RecordTypeTable, NewRecord("users").Build()).Build()).
				Build(),
		).
		WithCatch(
			NewBlock().
				WithStatement(NewMethodCall("logError").Build()).
				WithStatement(NewThrow(VarExpr("err"))).
				Build(),
		).
		Build()

	if tryStmt.GetStatementType() != ASTStatementTypeTry {
		t.Errorf("expected TryStmt type, got %v", tryStmt.GetStatementType())
	}

	if len(tryStmt.Body.Children) != 1 {
		t.Errorf("expected 1 statement in try body, got %d", len(tryStmt.Body.Children))
	}

	if len(tryStmt.Catch.Children) != 2 {
		t.Errorf("expected 2 statements in catch body, got %d", len(tryStmt.Catch.Children))
	}

	data, _ := json.Marshal(tryStmt)
	t.Logf("Try-catch statement: %s", string(data))
}

// TestHelperFunctions demonstrates using helper functions
func TestHelperFunctions(t *testing.T) {
	// Variable reference
	varRef := Var("userName")
	if varRef.Name != "userName" {
		t.Errorf("expected variable name 'userName', got %s", varRef.Name)
	}

	// Variable expression
	varExpr := VarExpr("userName")
	if varExpr.Variable == nil {
		t.Fatal("VarExpr should have Variable set")
	}
	if varExpr.Variable.Name != "userName" {
		t.Errorf("expected variable name 'userName', got %s", varExpr.Variable.Name)
	}

	// String literal
	strLit := StringLit("hello")
	if strLit.FieldType != RecordFieldTypeString {
		t.Errorf("expected string field type, got %v", strLit.FieldType)
	}
	if *strLit.Value != "hello" {
		t.Errorf("expected value 'hello', got %s", *strLit.Value)
	}

	// Boolean literal
	boolLit := BoolLit(true)
	if boolLit.FieldType != RecordFieldTypeBoolean {
		t.Errorf("expected boolean field type, got %v", boolLit.FieldType)
	}
	if *boolLit.Value != "true" {
		t.Errorf("expected value 'true', got %s", *boolLit.Value)
	}

	// Binary expression
	binExpr := BinaryExpr(
		VarExpr("x"),
		BinaryOpAdd,
		LitExpr("10", RecordFieldTypeNumber),
	)
	if binExpr.Binary == nil {
		t.Fatal("BinaryExpr should have Binary set")
	}
	if binExpr.Binary.Operator != BinaryOpAdd {
		t.Errorf("expected Add operator, got %v", binExpr.Binary.Operator)
	}

	// Unary expression
	unaryExpr := UnaryExpr(UnaryOpNot, VarExpr("active"))
	if unaryExpr.Unary == nil {
		t.Fatal("UnaryExpr should have Unary set")
	}
	if unaryExpr.Unary.Operator != UnaryOpNot {
		t.Errorf("expected Not operator, got %v", unaryExpr.Unary.Operator)
	}
}

// TestNewBinaryOperators tests the new string comparison operators
func TestNewBinaryOperators(t *testing.T) {
	// Test LIKE operator
	likeExpr := BinaryExpr(
		VarExpr("name"),
		BinaryOpLike,
		LitExpr("%john%", RecordFieldTypeString),
	)
	if likeExpr.Binary == nil {
		t.Fatal("BinaryExpr should have Binary set")
	}
	if likeExpr.Binary.Operator != BinaryOpLike {
		t.Errorf("expected Like operator, got %v", likeExpr.Binary.Operator)
	}

	// Test ILIKE operator
	ilikeExpr := BinaryExpr(
		VarExpr("email"),
		BinaryOpILike,
		LitExpr("%EXAMPLE.COM%", RecordFieldTypeString),
	)
	if ilikeExpr.Binary.Operator != BinaryOpILike {
		t.Errorf("expected ILike operator, got %v", ilikeExpr.Binary.Operator)
	}

	// Test REGEX operator
	regexExpr := BinaryExpr(
		VarExpr("phone"),
		BinaryOpRegex,
		LitExpr("^\\d{3}-\\d{4}$", RecordFieldTypeString),
	)
	if regexExpr.Binary.Operator != BinaryOpRegex {
		t.Errorf("expected Regex operator, got %v", regexExpr.Binary.Operator)
	}

	// Test STARTS_WITH operator
	startsWithExpr := BinaryExpr(
		VarExpr("username"),
		BinaryOpStartsWith,
		LitExpr("admin_", RecordFieldTypeString),
	)
	if startsWithExpr.Binary.Operator != BinaryOpStartsWith {
		t.Errorf("expected StartsWith operator, got %v", startsWithExpr.Binary.Operator)
	}

	// Test ENDS_WITH operator
	endsWithExpr := BinaryExpr(
		VarExpr("filename"),
		BinaryOpEndsWith,
		LitExpr(".pdf", RecordFieldTypeString),
	)
	if endsWithExpr.Binary.Operator != BinaryOpEndsWith {
		t.Errorf("expected EndsWith operator, got %v", endsWithExpr.Binary.Operator)
	}

	// Test CONTAINS operator
	containsExpr := BinaryExpr(
		VarExpr("description"),
		BinaryOpContains,
		LitExpr("urgent", RecordFieldTypeString),
	)
	if containsExpr.Binary.Operator != BinaryOpContains {
		t.Errorf("expected Contains operator, got %v", containsExpr.Binary.Operator)
	}

	// Test marshaling all operators
	operators := []BinaryOp{
		BinaryOpLike, BinaryOpILike, BinaryOpRegex,
		BinaryOpStartsWith, BinaryOpEndsWith, BinaryOpContains,
	}

	for _, op := range operators {
		expr := BinaryExpr(VarExpr("field"), op, LitExpr("value", RecordFieldTypeString))
		data, err := json.Marshal(expr)
		if err != nil {
			t.Errorf("failed to marshal %v operator: %v", op, err)
		}
		t.Logf("%v operator JSON: %s", op, string(data))
	}
}

// TestCastExpression tests the CastExpr helper function
func TestCastExpression(t *testing.T) {
	// Test basic cast
	castExpr := CastExpr(VarExpr("price"), "integer")

	// Verify structure
	if castExpr.Cast == nil {
		t.Fatal("CastExpr should have Cast set")
	}

	if castExpr.Cast.TargetType != "integer" {
		t.Errorf("expected targetType 'integer', got %s", castExpr.Cast.TargetType)
	}

	if castExpr.Cast.Expr.Variable == nil {
		t.Error("Cast expression should contain variable reference")
	}

	// Test marshaling
	data, err := json.Marshal(castExpr)
	if err != nil {
		t.Fatalf("failed to marshal cast expression: %v", err)
	}

	var unmarshaled ExprStmt
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("failed to unmarshal cast expression: %v", err)
	}

	if unmarshaled.Cast == nil {
		t.Fatal("Unmarshaled expression should have Cast set")
	}

	if unmarshaled.Cast.TargetType != "integer" {
		t.Errorf("expected unmarshaled targetType 'integer', got %s", unmarshaled.Cast.TargetType)
	}

	// Test pretty print
	pretty := castExpr.Pretty().ANSI()
	if pretty == "" {
		t.Error("Pretty output should not be empty")
	}
	t.Logf("Cast expression pretty print: %s", pretty)

	// Test complex cast (casting a literal)
	complexCast := CastExpr(LitExpr("123.45", RecordFieldTypeString), "float")
	if complexCast.Cast.Expr.Literal == nil {
		t.Error("Complex cast should contain literal")
	}
	if complexCast.Cast.Expr.Literal.FieldType != RecordFieldTypeString {
		t.Error("Literal should maintain original field type")
	}

	// Test nested expression with cast
	method := NewMethod("ConvertPrice").
		WithParam(Field("priceStr", RecordFieldTypeString)).
		WithReturn(Field("price", RecordFieldTypeFloat)).
		WithStatement(
			NewAssignment("price", CastExpr(VarExpr("priceStr"), "float")).Build(),
		).
		WithStatement(
			NewReturn(VarExpr("price")),
		).
		Build()

	if method.Body == nil {
		t.Fatal("Method should have body")
	}

	if len(method.Body.Children) != 2 {
		t.Errorf("expected 2 statements, got %d", len(method.Body.Children))
	}

	// Verify first statement is assignment with cast
	assignStmt, ok := method.Body.Children[0].(AssignmentStmt)
	if !ok {
		t.Fatalf("expected AssignmentStmt, got %T", method.Body.Children[0])
	}

	if assignStmt.Value.Cast == nil {
		t.Error("Assignment value should be a cast expression")
	}

	data, err = json.Marshal(method)
	if err != nil {
		t.Fatalf("failed to marshal method with cast: %v", err)
	}
	t.Logf("Method with cast JSON:\n%s", string(data))
}

// TestCompleteExample demonstrates building a complete realistic example
func TestCompleteExample(t *testing.T) {
	pkg := NewPackage("com.example.ecommerce").
		WithModule("github.com/example/ecommerce").
		WithPath("internal/services/order_service.go").
		WithVariable(Field("config", RecordFieldTypeObject)).
		WithVariable(Field("db", RecordFieldTypeObject)).
		WithType(
			NewType("OrderService").
				WithVisibility(VisibilityPublic).
				WithVariable(Field("repository", RecordFieldTypeObject)).
				WithVariable(Field("paymentClient", RecordFieldTypeObject)).
				WithVariable(Field("notifier", RecordFieldTypeObject)).
				WithMethod(
					NewMethod("CreateOrder").
						WithVisibility(VisibilityPublic).
						WithParam(Field("customerID", RecordFieldTypeString)).
						WithParam(Field("items", RecordFieldTypeArray)).
						WithReturn(Field("orderID", RecordFieldTypeString)).
						WithReturn(Field("error", RecordFieldTypeObject)).
						WithStatement(
							NewTry().
								WithBody(
									NewBlock().
										WithStatement(
											NewAssignment("order", VarExpr("newOrder")).Build(),
										).
										WithStatement(
											NewRecordWrite(RecordTypeTable, NewRecord("orders").Build()).
												WithArgument("data", VarExpr("order")).
												Build(),
										).
										WithStatement(
											NewEndpointCall(EndpointTypeHTTP, NewEndpoint("http://payment.example.com/charge").Build()).
												WithPositionalArg(VarExpr("order")).
												Build(),
										).
										WithStatement(
											NewEndpointCall(EndpointTypeKafka, NewEndpoint("order-created").Build()).
												WithPositionalArg(VarExpr("order")).
												Build(),
										).
										WithStatement(
											NewReturn(VarExpr("order.id")),
										).
										Build(),
								).
								WithCatch(
									NewBlock().
										WithStatement(
											NewMethodCall("logError").
												WithPositionalArg(VarExpr("err")).
												Build(),
										).
										WithStatement(
											NewReturn(LitExpr("", RecordFieldTypeString)),
										).
										Build(),
								).
								Build(),
						).
						Build(),
				).
				Build(),
		).
		Build()

	// Verify the complete structure
	if pkg.Package != "com.example.ecommerce" {
		t.Errorf("expected package name 'com.example.ecommerce', got %s", pkg.Package)
	}

	if len(pkg.Types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(pkg.Types))
	}

	orderService := pkg.Types[0]
	if orderService.Type != "OrderService" {
		t.Errorf("expected type name 'OrderService', got %s", orderService.Type)
	}

	if len(orderService.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(orderService.Methods))
	}

	createOrder := orderService.Methods[0]
	if createOrder.Method != "CreateOrder" {
		t.Errorf("expected method name 'CreateOrder', got %s", createOrder.Method)
	}

	if len(createOrder.Params) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(createOrder.Params))
	}

	if len(createOrder.Returns) != 2 {
		t.Errorf("expected 2 return values, got %d", len(createOrder.Returns))
	}

	// Marshal and verify
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal complete example: %v", err)
	}

	t.Logf("Complete example JSON:\n%s", string(data))
}
