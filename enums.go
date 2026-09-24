package uir

import (
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
)

type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityPrivate   Visibility = "private"
	VisibilityProtected Visibility = "protected"
	VisibilityInternal  Visibility = "internal"
	VisibilityPackage   Visibility = "package"
	VisibilityNA        Visibility = ""
)

type AssignmentOp string

const (
	AssignmentOpAssign          AssignmentOp = "="
	AssignmentOpAdd             AssignmentOp = "+="
	AssignmentOpSubtract        AssignmentOp = "-="
	AssignmentOpMultiply        AssignmentOp = "*="
	AssignmentOpDivide          AssignmentOp = "/="
	AssignmentOpModulus         AssignmentOp = "%="
	AssignmentOpAnd             AssignmentOp = "&="
	AssignmentOpOr              AssignmentOp = "|="
	AssignmentOpExponent        AssignmentOp = "**="
	AssignmentOpNullishCoalesce AssignmentOp = "??="
	AssignmentOpLogicalAnd      AssignmentOp = "&&="
	AssignmentOpLogicalOr       AssignmentOp = "||="
	// AssignmentOpAppend is a language-neutral "add to a collection" op.
	// Generators lower it per target language: TypeScript → `target.push(value)`
	// for arrays and `target += value` for strings; Python → `target.append(value)`
	// / `target += value`; Java → `target.add(value)` / `target += value`.
	// The literal value is the word "append" so it can never collide with a real
	// source-language operator.
	AssignmentOpAppend AssignmentOp = "append"
)

var AllAssignmentOps = []AssignmentOp{
	AssignmentOpAssign,
	AssignmentOpAdd,
	AssignmentOpSubtract,
	AssignmentOpMultiply,
	AssignmentOpDivide,
	AssignmentOpModulus,
	AssignmentOpAnd,
	AssignmentOpOr,
	AssignmentOpAppend,
}

type BinaryOp string

const (
	BinaryOpAnd               BinaryOp = "&&"
	BinaryOpOr                BinaryOp = "||"
	BinaryOpEqual             BinaryOp = "=="
	BinaryOpNotEqual          BinaryOp = "!="
	BinaryOpLess              BinaryOp = "<"
	BinaryOpLessEqual         BinaryOp = "<="
	BinaryOpGreater           BinaryOp = ">"
	BinaryOpGreaterEqual      BinaryOp = ">="
	BinaryOpAdd               BinaryOp = "+"
	BinaryOpSubtract          BinaryOp = "-"
	BinaryOpMultiply          BinaryOp = "*"
	BinaryOpDivide            BinaryOp = "/"
	BinaryOpModulus           BinaryOp = "%"
	BinaryOpLike              BinaryOp = "like"
	BinaryOpILike             BinaryOp = "ilike"
	BinaryOpRegex             BinaryOp = "regex"
	BinaryOpStartsWith        BinaryOp = "startsWith"
	BinaryOpEndsWith          BinaryOp = "endsWith"
	BinaryOpContains          BinaryOp = "contains"
	BinaryOpNullishCoalescing BinaryOp = "??"
	BinaryOpOptionalChain     BinaryOp = "?."
	BinaryOpIn                BinaryOp = "in"
	BinaryOpInstanceOf        BinaryOp = "instanceof"
	BinaryOpExponent          BinaryOp = "**"
	BinaryOpBitwiseAnd        BinaryOp = "&"
	BinaryOpBitwiseOr         BinaryOp = "|"
	BinaryOpBitwiseXor        BinaryOp = "^"
	BinaryOpLeftShift         BinaryOp = "<<"
	BinaryOpRightShift        BinaryOp = ">>"
	BinaryOpUnsignedRight     BinaryOp = ">>>"
	BinaryOpStrictEqual       BinaryOp = "==="
	BinaryOpStrictNotEqual    BinaryOp = "!=="
)

var AllBinaryOps = []BinaryOp{
	BinaryOpAnd,
	BinaryOpOr,
	BinaryOpEqual,
	BinaryOpNotEqual,
	BinaryOpLess,
	BinaryOpLessEqual,
	BinaryOpGreater,
	BinaryOpGreaterEqual,
	BinaryOpAdd,
	BinaryOpSubtract,
	BinaryOpMultiply,
	BinaryOpDivide,
	BinaryOpModulus,
	BinaryOpLike,
	BinaryOpILike,
	BinaryOpRegex,
	BinaryOpStartsWith,
	BinaryOpEndsWith,
	BinaryOpContains,
	BinaryOpNullishCoalescing,
	BinaryOpOptionalChain,
	BinaryOpIn,
	BinaryOpInstanceOf,
	BinaryOpExponent,
	BinaryOpBitwiseAnd,
	BinaryOpBitwiseOr,
	BinaryOpBitwiseXor,
	BinaryOpLeftShift,
	BinaryOpRightShift,
	BinaryOpUnsignedRight,
	BinaryOpStrictEqual,
	BinaryOpStrictNotEqual,
}

type UnaryOp string

const (
	UnaryOpNot      UnaryOp = "!"
	UnaryOpNeg      UnaryOp = "-"
	UnaryOpPlus     UnaryOp = "+"
	UnaryOpIncr     UnaryOp = "++"
	UnaryOpDecr     UnaryOp = "--"
	UnaryOpDeref    UnaryOp = "*"
	UnaryOpAddr     UnaryOp = "&"
	UnaryOpTypeof   UnaryOp = "typeof"
	UnaryOpVoid     UnaryOp = "void"
	UnaryOpDelete   UnaryOp = "delete"
	UnaryOpAwait    UnaryOp = "await"
	UnaryOpYield    UnaryOp = "yield"
	UnaryOpSpread   UnaryOp = "..."
	UnaryOpBitNot   UnaryOp = "~"
	UnaryOpKeyOf    UnaryOp = "keyof"
	UnaryOpReadonly UnaryOp = "readonly"
)

var AllUnaryOps = []UnaryOp{
	UnaryOpNot,
	UnaryOpNeg,
	UnaryOpPlus,
	UnaryOpIncr,
	UnaryOpDecr,
	UnaryOpDeref,
	UnaryOpAddr,
	UnaryOpTypeof,
	UnaryOpVoid,
	UnaryOpDelete,
	UnaryOpAwait,
	UnaryOpYield,
	UnaryOpSpread,
	UnaryOpBitNot,
	UnaryOpKeyOf,
	UnaryOpReadonly,
}

type RecordFieldType string

const (
	RecordFieldTypeString  RecordFieldType = "string"
	RecordFieldTypeNumber  RecordFieldType = "number"
	RecordFieldTypeBoolean RecordFieldType = "boolean"
	RecordFieldTypeArray   RecordFieldType = "array"
	RecordFieldTypeObject  RecordFieldType = "object"
	RecordFieldTypeEnum    RecordFieldType = "enum"
	RecordFieldTypeDate    RecordFieldType = "date"
	RecordFieldTypeFloat   RecordFieldType = "float"
	RecordFieldTypeInt     RecordFieldType = "int"
	RecordFieldTypeIP      RecordFieldType = "ip"
	RecordFieldTypeCIDR    RecordFieldType = "cidr"
	RecordFieldTypeURL     RecordFieldType = "url"
	RecordFieldTypeEmail   RecordFieldType = "email"
	RecordFieldTypeUUID    RecordFieldType = "uuid"
	RecordFieldTypeJSON    RecordFieldType = "json"
	RecordFieldTypeJSONB   RecordFieldType = "jsonb"
	RecordFieldTypeYAML    RecordFieldType = "yaml"
	RecordFieldTypeXML     RecordFieldType = "xml"
	RecordFieldTypeCSV     RecordFieldType = "csv"
	RecordFieldTypePhone   RecordFieldType = "phone"
	RecordFieldTypeText    RecordFieldType = "text"
	RecordFieldTypeSecret  RecordFieldType = "secret"
	RecordFieldTypeMap     RecordFieldType = "map"
)

type RecordReferenceType string

const (
	RecordReferenceTypeForeignKey RecordReferenceType = "foreignKey"
	// Similar semantics to foreign key but does not enforce referential integrity
	RecordReferenceTypeSoftKey RecordReferenceType = "softKey"
)

type RecordType string

const (
	// Record is used as the params to a method call, stored procedure, remote API call etc.
	RecordTypeFunctionInput RecordType = "input"
	// Record is used as the return value from a method call, stored procedure, remote API call etc.
	RecordTypeFunctionOutput RecordType = "output"
	RecordTypeTable          RecordType = "sql:table"
	RecordTypeView           RecordType = "sql:view"
	RecordTypeIndex          RecordType = "sql:index"
	RecordTypeStoredProc     RecordType = "sql:stored_proc"
	RecordTypeFunction       RecordType = "sql:function"
	RecordTypeTrigger        RecordType = "sql:trigger"
	RecordTypeCache          RecordType = "cache"
	RecordTypeYAML           RecordType = "file:yaml"
	RecordTypeJSON           RecordType = "file:json"
	RecordTypeXML            RecordType = "file:xml"
	RecordTypeCSV            RecordType = "file:csv"
	RecordTypeOther          RecordType = "other"
)

type ExpressionType string

const (
	// SQL expressions used in where clauses and constraints
	ExpressionTypeSQL      ExpressionType = "sql"
	ExpressionTypeXPath    ExpressionType = "xpath"
	ExpressionTypeJSONPath ExpressionType = "jsonpath"
	ExpressionTypeRegex    ExpressionType = "regex"
	ExpressionTypeJQ       ExpressionType = "jq"
	ExpressionTypeYQ       ExpressionType = "yq"
	ExpressionTypeJMESPath ExpressionType = "jmespath"
	ExpressionTypeCEL      ExpressionType = "cel"
	ExpressionTypeSpEL     ExpressionType = "SpEL"
	ExpressionTypeOGNL     ExpressionType = "ognl"
)

// CommentType represents the type of comment
type CommentType string

const (
	CommentTypeSingleLine    CommentType = "single_line"
	CommentTypeMultiLine     CommentType = "multi_line"
	CommentTypeDocumentation CommentType = "documentation"
)

// NodeType is an alias for backward compatibility
type NodeType string

// ASTNodeType constants for node types
const (

	// Top-level node types, each of these are represented as a distinct UIRNode in the database
	NodeTypePackage NodeType = "package" // Package, module, namespace, database schema etc.
	NodeTypeType    NodeType = "class"
	// A method is a type of functiomn associated with a class/type/package
	NodeTypeMethod NodeType = "method"
	// A module is a higher-level grouping of packages, e.g. Go module, Maven module, NPM package scope, Kubernetes Namespace
	NodeTypeModule NodeType = "module"
	// A record is a table-like structure, e.g. database table, API request/response schema, file schema etc.
	NodeTypeRecord NodeType = "record"
	// An endpoint represents an external system interaction point, e.g. API endpoint, message queue topic, database connection etc.
	NodeTypeEndpoint NodeType = "endpoint"
	// A function is a standalone callable unit, not associated with a class/type/package e.g. Lambda
	NodeTypeFunction NodeType = "function"
	// A field is a member/attribute of a record/type/class
	NodeTypeField NodeType = "record_field"
	// A table is a relational table or view. Unlike a record it carries the
	// structure a database actually has: typed columns, an ordered primary key,
	// indexes and foreign keys.
	NodeTypeTable NodeType = "table"
	// A column is a member of a table, keeping its dialect type alongside the
	// portable one.
	NodeTypeColumn NodeType = "column"
	// An index is a lookup structure over a table's columns
	NodeTypeIndex NodeType = "index"
	// A foreign key is a referential constraint from one table to another
	NodeTypeForeignKey NodeType = "foreign_key"

	// Secondary node types, these are represented as part of the parent UIRNode's content JSON

	// A dependency is a relationship between two nodes, indicating that one node relies on another
	NodeTypeDependency NodeType = "dependency"
	// An interface defines a contract for classes to implement
	NodeTypeInterface NodeType = "interface"
	// A constructor is a special method used to initialize a newly created object
	NodeTypeConstructor NodeType = "constructor"
	// A function variable is a variable that holds a reference to a function
	NodeTypeFunctionVariable NodeType = "function_variable"
	NodeTypePackageVariable  NodeType = "package_variable"
	NodeTypeImport           NodeType = "import"
	NodeTypeAnnotation       NodeType = "annotation"
	NodeTypeComment          NodeType = "comment"
	// A ref is a NodeRef: a pointer to another node by identifier. It is the kind a
	// reference is encoded under, never the kind of the node it points at, which
	// stays on the reference's own Identifier.NodeType.
	NodeTypeRef     NodeType = "ref"
	NodeTypeUnknown NodeType = ""
)

func (n NodeType) Color() string {
	switch n {
	case NodeTypePackage, NodeTypeModule:
		return "text-emerald-500"
	case NodeTypeType:
		return "text-blue-500"
	case NodeTypeInterface:
		return "text-cyan-500"
	case NodeTypeMethod, NodeTypeFunction:
		return "text-purple-500"
	case NodeTypeRecord, NodeTypeTable:
		return "text-orange-500"
	case NodeTypeField, NodeTypeColumn:
		return "text-amber-500"
	case NodeTypeIndex:
		return "text-purple-400"
	case NodeTypeForeignKey:
		return "text-teal-400"
	case NodeTypeEndpoint:
		return "text-gray-500"
	case NodeTypeFunctionVariable, NodeTypePackageVariable:
		return "text-amber-500"
	case NodeTypeAnnotation:
		return "text-violet-400"
	case NodeTypeImport, NodeTypeDependency:
		return "text-teal-500"
	case NodeTypeUnknown, NodeTypeComment:
		return "text-gray-400"
	default:
		return ""
	}
}

func (n NodeType) Icon() api.Textable {
	switch n {
	case NodeTypePackage, NodeTypeModule:
		return icons.Package
	case NodeTypeType:
		return icons.Type
	case NodeTypeInterface:
		return icons.Interface
	case NodeTypeMethod, NodeTypeFunction:
		return icons.Method
	case NodeTypeRecord:
		return icons.Table
	case NodeTypeTable:
		return icons.Database
	case NodeTypeIndex:
		return icons.Key
	case NodeTypeForeignKey:
		return icons.Link
	case NodeTypeField, NodeTypeColumn, NodeTypeFunctionVariable, NodeTypePackageVariable:
		return icons.Variable
	case NodeTypeEndpoint:
		return icons.Http

	}
	return api.Text{Content: ""}
}

var AllNodeTypes = []NodeType{
	NodeTypePackage,
	NodeTypeType,
	NodeTypeMethod,
	NodeTypeDependency,
	NodeTypeModule,
	NodeTypeRecord,
	NodeTypeEndpoint,
	NodeTypeField,
	NodeTypeTable,
	NodeTypeColumn,
	NodeTypeIndex,
	NodeTypeForeignKey,
	NodeTypeFunction,
	NodeTypeUnknown,
}

type FlowType string

const (
	FlowTypeInbound  FlowType = "inbound"
	FlowTypeOutbound FlowType = "outbound"
	FlowTypeProxy    FlowType = "proxy"
	FlowTypeNA       FlowType = ""
)

type StatementType string

type StatementCategory string

const (
	StatementCategoryCall  StatementCategory = "call"
	StatementCategoryRead  StatementCategory = "read"
	StatementCategoryWrite StatementCategory = "write"

	StatementCategoryControlFlow    StatementCategory = "control"
	StatementCategoryDataAssignment StatementCategory = "assignment"
	StatementCategoryLookup         StatementCategory = "lookup"
	StatementCategoryOther          StatementCategory = "other"
	StatementCategoryDeclaration    StatementCategory = "decl"
)

var AllCategoriesStatement = []StatementCategory{
	StatementCategoryCall,
	StatementCategoryRead,
	StatementCategoryWrite,
	StatementCategoryControlFlow,
	StatementCategoryDataAssignment,
	StatementCategoryLookup,
	StatementCategoryDeclaration,
	StatementCategoryOther,
}

const (
	NodeTypeStatement NodeType = "statement"
	// Assumed to be a function call to a CatergoryTypeMethod, local to the class/package
	ASTStatementTypeCall StatementType = StatementType(StatementCategoryCall)

	ASTStatementTypeDecl StatementType = StatementType(StatementCategoryDeclaration)

	// A call to an external package, library or module
	ASTStatementTypeCallPackage StatementType = ASTStatementTypeCall + ":package"
	ASTStatementTypeRemoteRead  StatementType = ASTStatementTypeCall + ":read"
	ASTStatementTypeRemoteWrite StatementType = ASTStatementTypeCall + ":write"
	ASTStatementTypeRecordRead  StatementType = ASTStatementTypeRemoteRead + ":record"
	ASTStatementTypeRecordWrite StatementType = ASTStatementTypeRemoteWrite + ":record"

	ASTStatementTypeImport          StatementType = StatementType(ASTStatementTypeDecl + ":import")
	ASTStatementTypeDeclareVariable StatementType = StatementType(ASTStatementTypeDecl + ":var")
	ASTStatementTypeDeclareConstant StatementType = StatementType(ASTStatementTypeDecl + ":const")
	ASTStatementTypeDeclareFunction StatementType = StatementType(ASTStatementTypeDecl + ":func")
	ASTStatementTypeDeclareType     StatementType = StatementType(ASTStatementTypeDecl + ":type")
	ASTStatementTypeExport          StatementType = StatementType(ASTStatementTypeDecl + ":export")

	ASTStatementRef           StatementType = "ref"
	ASTStatementRefScopeVar   StatementType = ASTStatementRef + ":var"
	ASTStatementRefPackageVar StatementType = ASTStatementRef + ":package"
	ASTStatementRefEndpoint   StatementType = ASTStatementRef + ":endpoint"
	ASTStatementRefRecord     StatementType = ASTStatementRef + ":record"

	ASTStatementTypeKafkaConsumerCall StatementType = ASTStatementTypeRemoteRead + StatementType(":"+EndpointTypeKafka)
	ASTStatementTypeKafkaProducerCall StatementType = ASTStatementTypeRemoteWrite + StatementType(":"+EndpointTypeKafka)
	ASTStatementTypeJMSConsumerCall   StatementType = ASTStatementTypeRemoteRead + StatementType(":"+EndpointTypeJMS)
	ASTStatementTypeJMSProducerCall   StatementType = ASTStatementTypeRemoteWrite + StatementType(":"+EndpointTypeJMS)
	ASTStatementTypeFileRead          StatementType = ASTStatementTypeRemoteRead + ":file"

	ASTStatementTypeFileWrite   StatementType = ASTStatementTypeRemoteWrite + ":file"
	ASTStatementTypeCallAPI     StatementType = ASTStatementTypeCall + ":api"
	ASTStatementTypeHttpCall    StatementType = ASTStatementTypeCallAPI + ":http"
	ASTStatementTypeSoapCall    StatementType = ASTStatementTypeCallAPI + ":soap"
	ASTStatementTypeGRPCCall    StatementType = ASTStatementTypeCallAPI + ":grpc"
	ASTStatementTypeGraphQLCall StatementType = ASTStatementTypeCallAPI + ":graphql"
	ASTStatementTypeTCPCall     StatementType = ASTStatementTypeCallAPI + ":tcp"

	ASTStatementTypeOther StatementType = StatementType(StatementCategoryOther)

	ASTStatementTypeIf          StatementType = StatementType(StatementCategoryControlFlow + ":if")
	ASTStatementTypeLoop        StatementType = StatementType(StatementCategoryControlFlow + ":loop")
	ASTStatementTypeLoopFor     StatementType = ASTStatementTypeLoop + ":for"
	ASTStatementTypeLoopForEach StatementType = ASTStatementTypeLoop + ":foreach"
	ASTStatementTypeLoopWhile   StatementType = ASTStatementTypeLoop + ":while"
	ASTStatementTypeSwitch      StatementType = StatementType(StatementCategoryControlFlow + ":switch")
	ASTStatementTypeThrow       StatementType = StatementType(StatementCategoryControlFlow + ":throw")
	ASTStatementTypeTry         StatementType = StatementType(StatementCategoryControlFlow + ":try")
	ASTStatementTypeBlock       StatementType = StatementType(StatementCategoryControlFlow + ":block")
	ASTStatmentTypeCondition    StatementType = StatementType(StatementCategoryControlFlow + ":condition")
	ASTStatementTypeReturn      StatementType = StatementType(StatementCategoryControlFlow + ":return")
	ASTStatementTypeBreak       StatementType = StatementType(StatementCategoryControlFlow + ":break")
	ASTStatementTypeContinue    StatementType = StatementType(StatementCategoryControlFlow + ":continue")

	ASTStatementTypeExpression    StatementType = StatementType(StatementCategoryDataAssignment + ":expression")
	ASTStatementTypeAssignment    StatementType = StatementType(StatementCategoryDataAssignment)
	ASTStatementTypeLiteral       StatementType = StatementType(StatementCategoryDataAssignment + ":literal")
	ASTStatementTypeTuple         StatementType = StatementType(StatementCategoryDataAssignment + ":tuple")
	ASTStatementTypeObjectLiteral StatementType = StatementType(StatementCategoryDataAssignment + ":object")
	ASTStatementTypeVariable      StatementType = StatementType(StatementCategoryDataAssignment + ":variable")
	ASTStatementTypeArray         StatementType = StatementType(StatementCategoryDataAssignment + ":array")
	ASTStatementTypeBinary        StatementType = StatementType(StatementCategoryDataAssignment + ":binary")
	ASTStatementTypeUnary         StatementType = StatementType(StatementCategoryDataAssignment + ":unary")
	ASTStatementTypeCast          StatementType = StatementType(StatementCategoryDataAssignment + ":cast")
	ASTStatementTypeDoc           StatementType = StatementType("doc")
	ASTStatementTypeTest          StatementType = StatementType("test")
	ASTStatementTypeDestructure   StatementType = StatementType(StatementCategoryDataAssignment + ":destructure")
	ASTStatementTypeRaw           StatementType = StatementType("raw")
	ASTStatementTypeTemplateLit   StatementType = StatementType(StatementCategoryDataAssignment + ":template")
	ASTStatementTypeForeignKey    StatementType = StatementType("foreign_key")
)

type RelationshipType string

const (
	RelationshipTypeImport      RelationshipType = "import"      // Import statement in code
	RelationshipTypeCall        RelationshipType = "call"        // Function/method call
	RelationshipTypeReference   RelationshipType = "reference"   // Variable reference
	RelationshipTypeInheritance RelationshipType = "inheritance" // Class inheritance, Docker FROM
	RelationshipTypeImplements  RelationshipType = "implements"  // Interface implementation
	RelationshipTypeIncludes    RelationshipType = "includes"    // e.g. For a chart including a subchart
	RelationshipTypeForeignKey  RelationshipType = "foreign_key" // Database foreign key constraint
	RelationshipTypeRead        RelationshipType = "read"        // Read operation from a record/data source
	RelationshipTypeWrite       RelationshipType = "write"       // Write operation to a record/data source
	RelationshipTypeNA          RelationshipType = ""
)

type FieldType string

const (
	FieldTypeString  FieldType = "string"
	FieldTypeNumber  FieldType = "number"
	FieldTypeBoolean FieldType = "boolean"
	FieldTypeArray   FieldType = "array"
	FieldTypeObject  FieldType = "object"
	FieldTypeEnum    FieldType = "enum"
	FieldTypeDate    FieldType = "date"
	FieldTypeFloat   FieldType = "float"
	FieldTypeMap     FieldType = "map"
	// Result of an expression evaluation
	FieldTypeExpression FieldType = "expression"
	// Result of a SQL query
	FieldTypeSQL FieldType = "sql"
	// i.e. lambda, closure, arrow function, etc.
	FieldTypeFunction FieldType = "function"
	// Result of a method call
	FieldTypeMethodResult FieldType = "method_result"
	// XPath expression
	FieldTypeXPath FieldType = "xpath"
)

type EndpointType string

const (
	EndpointTypeSOAP    EndpointType = "soap"
	EndpointTypeJMS     EndpointType = "jms"
	EndpointTypeKafka   EndpointType = "kafka"
	EndpointTypeTCP     EndpointType = "tcp"
	EndpointTypeHTTP    EndpointType = "http"
	EndpointTypeGRPC    EndpointType = "grpc"
	EndpointTypeGraphQL EndpointType = "graphql"
)

func (e EndpointType) String() string {
	return string(e)
}

type FunctionType string

const (
	FunctionTypeStatic    FunctionType = "static"
	FunctionTypeAnonymous FunctionType = "anonymous"
	// A function that is tied to an instance of a type, e.g. a method
	FunctionTypeInstance FunctionType = "instance"
)

type ASTErrorType string

const (
	ASTErrorTypeSyntax        ASTErrorType = "syntax"
	ASTErrorTypeValidation    ASTErrorType = "validation"
	ASTErrorTypeCompilation   ASTErrorType = "compilation"
	ASTErrorTypeRuntime       ASTErrorType = "runtime"
	ASTErrorTypeConfiguration ASTErrorType = "configuration"
	ASTErrorTypeNetwork       ASTErrorType = "network"
	ASTErrorTypeAuth          ASTErrorType = "auth"
	ASTErrorTypeAuthZ         ASTErrorType = "authz"
	ASTErrorTypeTimeout       ASTErrorType = "timeout"
)

func (a StatementType) String() string {
	return string(a)
}
