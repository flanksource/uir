package uir

import (
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
)

// The styles rendering applies, as the class strings api.Text takes. A name is a
// role, not a colour: two roles that share a class today are still two names, so
// that restyling one does not restyle the other.
const (
	StyleKeyword         = "text-blue-500"
	StyleName            = "text-green-600"
	StyleDeclarationName = "text-green-600 font-bold"
	StyleTypeName        = "text-blue-500"
	StylePunctuation     = "text-gray-600"
	StyleSecondary       = "text-gray-600"
	StyleDim             = "text-gray-500"
	StyleFaint           = "text-gray-400"
	StyleMuted           = "muted"
	StyleTextMuted       = "text-muted"
	StyleBold            = "font-bold"
	StyleWarning         = "text-amber-500"
	StyleComment         = "text-gray-600 italic"

	StyleOperator       = "text-orange-500"
	StyleBinaryOperator = "text-orange-600"
	StyleControlFlow    = "text-red-500"
	StyleContinue       = "text-yellow-500"
	StyleThrow          = "text-red-600"
	StyleTry            = "text-blue-600"
	StyleObjectKey      = "text-cyan-600"
	StyleArgumentName   = "text-yellow-600"
	StyleQuery          = "text-green-500"

	StyleLiteralString  = "text-green-600"
	StyleLiteralNumber  = "text-blue-600"
	StyleLiteralBoolean = "text-yellow-600"
	StyleLiteralOther   = "text-orange-600"

	StyleTypedString  = "text-green-500"
	StyleTypedNumber  = "text-blue-500"
	StyleTypedBoolean = "text-green-500"
	StyleTypedDate    = "text-yellow-500"
	StyleValue        = "text-orange-600"
	StyleValueAssign  = "text-gray-400 font-mono"

	StyleEndpoint     = "text-blue-500"
	StyleEndpointType = "text-purple-600"
	StyleEnvironment  = "text-blue-600"
	StyleFileName     = "text-blue-500 font-medium"
	StyleLineMarker   = "text-gray-500 text-xs"
	StyleLineNumber   = "text-purple-600 font-mono"

	StyleSQLType    = "text-blue-400"
	StylePrimaryKey = "text-amber-500"
	StyleIdentity   = "text-amber-400"
	StyleIndexKind  = "text-purple-500"
	StyleForeignKey = "text-teal-500"
)

// LiteralStyle is the style of a literal of the given type: one each for
// strings, numbers and booleans, and one for every other type.
func LiteralStyle(fieldType RecordFieldType) string {
	switch fieldType {
	case RecordFieldTypeString:
		return StyleLiteralString
	case RecordFieldTypeNumber, RecordFieldTypeFloat, RecordFieldTypeInt:
		return StyleLiteralNumber
	case RecordFieldTypeBoolean:
		return StyleLiteralBoolean
	}
	return StyleLiteralOther
}

// typeNameTheme is how a type name is printed. An empty label prints the type's
// own name.
type typeNameTheme struct {
	label string
	style string
}

var recordFieldTypeThemes = map[RecordFieldType]typeNameTheme{
	RecordFieldTypeString:  {style: StyleLiteralString},
	RecordFieldTypeNumber:  {style: StyleLiteralNumber},
	RecordFieldTypeFloat:   {style: StyleLiteralNumber},
	RecordFieldTypeInt:     {style: StyleLiteralNumber},
	RecordFieldTypeBoolean: {style: StyleLiteralBoolean},
	RecordFieldTypeArray:   {style: "text-purple-600"},
	RecordFieldTypeObject:  {style: "text-cyan-600"},
	RecordFieldTypeMap:     {label: "object", style: "text-cyan-600"},
	RecordFieldTypeEnum:    {style: "text-red-600"},
	RecordFieldTypeDate:    {style: "text-indigo-600"},
	RecordFieldTypeIP:      {style: "text-teal-600"},
	RecordFieldTypeCIDR:    {style: "text-teal-600"},
}

const fieldTypeDefaultStyle = "text-yellow-600"

var fieldTypeThemes = map[FieldType]typeNameTheme{
	FieldTypeString:  {style: "text-green-600"},
	FieldTypeNumber:  {style: "text-blue-600"},
	FieldTypeFloat:   {label: "number", style: "text-blue-600"},
	FieldTypeBoolean: {style: "text-red-600"},
	FieldTypeArray:   {style: "text-purple-600"},
	FieldTypeObject:  {style: "text-pink-600"},
	FieldTypeEnum:    {style: "text-indigo-600"},
	FieldTypeDate:    {style: "text-gray-600"},
	FieldTypeMap:     {style: "text-teal-600"},
	FieldTypeXPath:   {style: "text-orange-600"},
}

var nodeTypeStyles = map[NodeType]string{
	NodeTypePackage:          "text-emerald-500",
	NodeTypeModule:           "text-emerald-500",
	NodeTypeType:             "text-blue-500",
	NodeTypeInterface:        "text-cyan-500",
	NodeTypeMethod:           "text-purple-500",
	NodeTypeFunction:         "text-purple-500",
	NodeTypeRecord:           "text-orange-500",
	NodeTypeTable:            "text-orange-500",
	NodeTypeField:            "text-amber-500",
	NodeTypeColumn:           "text-amber-500",
	NodeTypeIndex:            "text-purple-400",
	NodeTypeForeignKey:       "text-teal-400",
	NodeTypeEndpoint:         "text-gray-500",
	NodeTypeFunctionVariable: "text-amber-500",
	NodeTypePackageVariable:  "text-amber-500",
	NodeTypeAnnotation:       "text-violet-400",
	NodeTypeImport:           "text-teal-500",
	NodeTypeDependency:       "text-teal-500",
	NodeTypeUnknown:          "text-gray-400",
	NodeTypeComment:          "text-gray-400",
}

var nodeTypeIcons = map[NodeType]api.Textable{
	NodeTypePackage:          icons.Package,
	NodeTypeModule:           icons.Package,
	NodeTypeType:             icons.Type,
	NodeTypeInterface:        icons.Interface,
	NodeTypeMethod:           icons.Method,
	NodeTypeFunction:         icons.Method,
	NodeTypeRecord:           icons.Table,
	NodeTypeTable:            icons.Database,
	NodeTypeIndex:            icons.Key,
	NodeTypeForeignKey:       icons.Link,
	NodeTypeField:            icons.Variable,
	NodeTypeColumn:           icons.Variable,
	NodeTypeFunctionVariable: icons.Variable,
	NodeTypePackageVariable:  icons.Variable,
	NodeTypeEndpoint:         icons.Http,
}

type relationshipTheme struct {
	icon  api.Textable
	label string
	style string
}

var referenceRelationshipTheme = relationshipTheme{icon: icons.ArrowRight, label: " reference", style: "text-yellow-600"}

var relationshipThemes = map[RelationshipType]relationshipTheme{
	RelationshipTypeImport:      {icon: icons.ArrowDown, label: " import", style: "text-blue-600"},
	RelationshipTypeCall:        {icon: icons.ArrowRight, label: " call", style: "text-green-600"},
	RelationshipTypeDispatch:    {icon: icons.ArrowRight, label: " dispatch", style: "text-green-600"},
	RelationshipTypeInheritance: {icon: icons.ArrowRight, label: " extends", style: "text-purple-600"},
	RelationshipTypeImplements:  {icon: icons.ArrowRight, label: " implements", style: "text-indigo-600"},
	RelationshipTypeIncludes:    {icon: icons.ArrowRight, label: " includes", style: "text-pink-600"},
	RelationshipTypeForeignKey:  {icon: icons.ArrowRight, label: " foreign key", style: "text-red-600"},
}
