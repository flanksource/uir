package uir

// PackageBuilder provides fluent API for building PackageNode
// PackageBuilder provides fluent API for building PackageNode
type PackageBuilder struct {
	node PackageNode
}

func (b PackageBuilder) AsNode() UIRNode {
	return UIRNode{
		Package: &b.node,
	}
}

func (b ModuleBuilder) AsNode() UIRNode {
	return UIRNode{
		Module: &b.node,
	}
}

func (b TypeBuilder) AsNode() UIRNode {
	return UIRNode{
		Type: &b.node,
	}
}

func (b MethodBuilder) AsNode() UIRNode {
	return UIRNode{
		Function: &b.node,
	}
}

func (v RecordBuilder) AsNode() UIRNode {
	return UIRNode{
		Record: &v.node,
	}
}

func (e EndpointBuilder) AsNode() UIRNode {
	return UIRNode{
		Endpoint: &e.node,
	}
}

func (b TypeBuilder) WithSource(path string, start, end int) *TypeBuilder {
	b.node.Path = path
	b.node.StartLine = &start
	b.node.EndLine = &end
	return &b
}

func (b PackageBuilder) WithSource(path string, start, end int) *PackageBuilder {
	b.node.Path = path
	b.node.StartLine = &start
	b.node.EndLine = &end
	return &b
}

func (b MethodBuilder) WithSource(path string, start, end int) *MethodBuilder {
	b.node.Path = path
	b.node.StartLine = &start
	b.node.EndLine = &end
	return &b
}

func (b MethodBuilder) WithProperties(keysAndVals ...any) *MethodBuilder {
	if b.node.Properties == nil {
		b.node.Properties = make(map[string]any)
	}
	for i := 0; i < len(keysAndVals); i += 2 {
		if i+1 < len(keysAndVals) {
			key := keysAndVals[i].(string)
			value := keysAndVals[i+1]
			b.node.Properties[key] = value
		}
	}
	return &b
}

func (b EndpointBuilder) WithSource(path string, start, end int) *EndpointBuilder {
	b.node.Path = path
	b.node.StartLine = &start
	b.node.EndLine = &end
	return &b
}

func (b RecordBuilder) WithSource(path string, start, end int) *RecordBuilder {
	b.node.Path = path
	b.node.StartLine = &start
	b.node.EndLine = &end
	return &b
}

func NewPackage(name string, id ...Identifier) *PackageBuilder {
	val := PackageNode{}
	if len(id) > 0 {
		val.Identifier = id[0]
	}
	if val.NodeType == "" {
		val.NodeType = NodeTypePackage
	}
	val.Package = name

	return &PackageBuilder{
		node: val,
	}
}

func (b *PackageBuilder) WithModule(module string) *PackageBuilder {
	b.node.Module = module
	return b
}

func (b *PackageBuilder) WithPath(path string) *PackageBuilder {
	b.node.Path = path
	return b
}

func (b *PackageBuilder) WithLanguage(lang string) *PackageBuilder {
	b.node.Language = &lang
	return b
}

func (b *PackageBuilder) WithVariable(field RecordField) *PackageBuilder {
	b.node.Variables = append(b.node.Variables, field)
	return b
}

func (b *PackageBuilder) WithVariables(fields ...RecordField) *PackageBuilder {
	b.node.Variables = append(b.node.Variables, fields...)
	return b
}

func (b *PackageBuilder) WithFunction(fn MethodNode) *PackageBuilder {
	b.node.Functions = append(b.node.Functions, fn)
	return b
}

func (b *PackageBuilder) WithType(typ TypedNode) *PackageBuilder {
	b.node.Types = append(b.node.Types, typ)
	return b
}

func (b *PackageBuilder) WithRecord(rec ASTRecord) *PackageBuilder {
	b.node.Records = append(b.node.Records, rec)
	return b
}

func (b *PackageBuilder) WithEndpoint(endpoint ASTEndpoint) *PackageBuilder {
	b.node.Endpoints = append(b.node.Endpoints, endpoint)
	return b
}

func (b *PackageBuilder) WithInitFunction(fn MethodNode) *PackageBuilder {
	b.node.InitFunctions = append(b.node.InitFunctions, fn)
	return b
}

func (b *PackageBuilder) WithAnnotation(annotation Annotation) *PackageBuilder {
	b.node.Annotations = append(b.node.Annotations, annotation)
	return b
}

func (b *PackageBuilder) Build() *PackageNode {
	return &b.node
}

// TypeBuilder provides fluent API for building TypedNode
type TypeBuilder struct {
	node TypedNode
}

func NewType(name string) *TypeBuilder {
	val := TypedNode{}
	val.Type = name
	val.NodeType = NodeTypeType
	return &TypeBuilder{
		node: val,
	}
}

func (b *TypeBuilder) WithPackage(pkg string) *TypeBuilder {
	b.node.Package = pkg
	return b
}

func (b *TypeBuilder) WithModule(module string) *TypeBuilder {
	b.node.Module = module
	return b
}

func (b *TypeBuilder) WithVisibility(vis Visibility) *TypeBuilder {
	b.node.Visibility = vis
	return b
}

func (b *TypeBuilder) WithPath(path string) *TypeBuilder {
	b.node.Path = path
	return b
}

func (b *TypeBuilder) WithVariable(field RecordField) *TypeBuilder {
	b.node.Variables = append(b.node.Variables, field)
	return b
}

func (b *TypeBuilder) WithVariables(fields ...RecordField) *TypeBuilder {
	b.node.Variables = append(b.node.Variables, fields...)
	return b
}

func (b *TypeBuilder) WithMethod(method MethodNode) *TypeBuilder {
	b.node.Methods = append(b.node.Methods, method)
	return b
}

func (b *TypeBuilder) WithMethods(methods ...MethodNode) *TypeBuilder {
	b.node.Methods = append(b.node.Methods, methods...)
	return b
}

func (b *TypeBuilder) WithImplements(refs ...TypeReference) *TypeBuilder {
	b.node.Implements = append(b.node.Implements, refs...)
	return b
}

func (b *TypeBuilder) WithExtends(ref TypeReference) *TypeBuilder {
	b.node.Extends = &ref
	return b
}

func (b *TypeBuilder) WithTypeParams(params ...TypeParam) *TypeBuilder {
	b.node.TypeParams = append(b.node.TypeParams, params...)
	return b
}

func (b *TypeBuilder) WithAbstract(abstract bool) *TypeBuilder {
	b.node.IsAbstract = abstract
	return b
}

func (b *TypeBuilder) WithConstructor(method MethodNode) *TypeBuilder {
	b.node.Constructor = &method
	return b
}

func (b *TypeBuilder) WithDestructor(method MethodNode) *TypeBuilder {
	b.node.Destructor = &method
	return b
}

func (b *TypeBuilder) WithInnerType(typ TypedNode) *TypeBuilder {
	b.node.Types = append(b.node.Types, typ)
	return b
}

func (b *TypeBuilder) WithAnnotation(annotation Annotation) *TypeBuilder {
	b.node.Annotations = append(b.node.Annotations, annotation)
	return b
}

func (b *TypeBuilder) Build() TypedNode {
	return b.node
}

// MethodBuilder provides fluent API for building MethodNode
type MethodBuilder struct {
	node MethodNode
	body *BlockStmt
}

type RecordBuilder struct {
	node ASTRecord
}

func (r *RecordBuilder) Build() ASTRecord {
	return r.node
}

type EndpointBuilder struct {
	node ASTEndpoint
}

func (e *EndpointBuilder) Build() ASTEndpoint {
	return e.node
}

func NewEndpoint(name string, id ...Identifier) *EndpointBuilder {
	val := ASTEndpoint{}
	if len(id) > 0 {
		val.Identifier = id[0]
	}
	val.Identifier.Method = name
	if val.NodeType == "" {
		val.NodeType = NodeTypeEndpoint
	}

	return &EndpointBuilder{
		node: val,
	}
}

func NewRecord(name string, id ...Identifier) *RecordBuilder {
	val := ASTRecord{}
	if len(id) > 0 {
		val.Identifier = id[0]
	}
	val.Type = name
	if val.NodeType == "" {
		val.NodeType = NodeTypeRecord
	}

	return &RecordBuilder{
		node: val,
	}
}

func NewModule(name string) *ModuleBuilder {
	val := ModuleNode{}
	val.Module = name
	val.NodeType = NodeTypeModule
	return &ModuleBuilder{
		node: val,
	}
}

type ModuleBuilder struct {
	node ModuleNode
}

func (m *ModuleBuilder) WithPackages(nodes ...PackageNode) *ModuleBuilder {
	m.node.Packages = append(m.node.Packages, nodes...)
	return m
}

func (m *ModuleBuilder) Build() ModuleNode {
	return m.node
}

func NewMethod(name string, id ...Identifier) *MethodBuilder {
	val := MethodNode{}
	if len(id) > 0 {
		val.Identifier = id[0]
	}
	// Set Method after Identifier so it doesn't get overwritten
	val.Method = name
	if val.NodeType == "" {
		val.NodeType = NodeTypeMethod
	}

	return &MethodBuilder{node: val}
}

func (b *MethodBuilder) WithType(typeName string) *MethodBuilder {
	b.node.Type = typeName
	return b
}

func (b *MethodBuilder) WithPackage(pkg string) *MethodBuilder {
	b.node.Package = pkg
	return b
}

func (b *MethodBuilder) WithModule(module string) *MethodBuilder {
	b.node.Module = module
	return b
}

func (b *MethodBuilder) WithVisibility(vis Visibility) *MethodBuilder {
	b.node.Visibility = vis
	return b
}

func (b *MethodBuilder) WithPath(path string) *MethodBuilder {
	b.node.Path = path
	return b
}

type RecordFieldBuilder struct {
	field RecordField
}

func (b *RecordFieldBuilder) Build() RecordField {
	return b.field
}

func (b *RecordFieldBuilder) WithDefaultValue(value TypedValue) *RecordFieldBuilder {
	b.field.DefaultValue = &value
	return b
}

func (b *RecordFieldBuilder) WithAnnotation(annotation Annotation) *RecordFieldBuilder {
	b.field.Annotations = append(b.field.Annotations, annotation)
	return b
}

func (b *RecordFieldBuilder) WithValidation(validation FieldValidation) *RecordFieldBuilder {
	b.field.Validation = validation
	return b
}

func (b *RecordFieldBuilder) WithValidationRule(rule ValidationRule) *RecordFieldBuilder {
	b.field.Validation.Rules = append(b.field.Validation.Rules, rule)
	return b
}

func (b *RecordFieldBuilder) WithValidationHint(hint GeneratorHint) *RecordFieldBuilder {
	b.field.Validation.Hints = append(b.field.Validation.Hints, hint)
	return b
}

// WithReadOnly sets the readOnly flag so the TS emitter prepends `readonly`.
func (b *RecordFieldBuilder) WithReadOnly() *RecordFieldBuilder {
	b.field.ReadOnly = true
	return b
}

// WithStatic marks the field as `static` for class emitters that support it.
// The flag rides through the generic Metadata.Properties map keyed "isStatic"
// so it propagates across the proto bridge without a schema change.
func (b *RecordFieldBuilder) WithStatic() *RecordFieldBuilder {
	if b.field.Properties == nil {
		b.field.Properties = map[string]any{}
	}
	b.field.Properties["isStatic"] = true
	return b
}

// WithRawInitializer attaches a verbatim TS initializer expression that
// the emitter writes after `=`, skipping the type annotation. Used for
// nested object literals that don't fit the primitive TypedValue shape
// (e.g. `{ guid: "...", name: "..." } as const`).
func (b *RecordFieldBuilder) WithRawInitializer(expr string) *RecordFieldBuilder {
	if b.field.Properties == nil {
		b.field.Properties = map[string]any{}
	}
	b.field.Properties["rawInitializer"] = expr
	return b
}

func NewRecordField(name string, fieldType RecordFieldType, id ...Identifier) *RecordFieldBuilder {
	val := RecordField{FieldType: fieldType}
	if len(id) > 0 {
		val.Identifier = id[0]
	}
	if val.Field == "" {
		val.Field = name
	}

	return &RecordFieldBuilder{
		field: val,
	}
}

func (b *RecordBuilder) WithValidation(validation RecordValidation) *RecordBuilder {
	b.node.Validation = validation
	return b
}

func (b *MethodBuilder) WithParam(field RecordField) *MethodBuilder {
	b.node.Params = append(b.node.Params, field)
	return b
}

func (b *MethodBuilder) WithParams(fields ...RecordField) *MethodBuilder {
	b.node.Params = append(b.node.Params, fields...)
	return b
}

func (b *MethodBuilder) WithReturn(field RecordField) *MethodBuilder {
	b.node.Returns = append(b.node.Returns, field)
	return b
}

func (b *MethodBuilder) WithReturns(fields ...RecordField) *MethodBuilder {
	b.node.Returns = append(b.node.Returns, fields...)
	return b
}

func (b *MethodBuilder) WithError(err ASTError) *MethodBuilder {
	b.node.Errors = append(b.node.Errors, err)
	return b
}

func (b *MethodBuilder) WithReturnType(ref TypeReference) *MethodBuilder {
	b.node.ReturnType = &ref
	return b
}

func (b *MethodBuilder) WithTypeParams(params ...TypeParam) *MethodBuilder {
	b.node.TypeParams = append(b.node.TypeParams, params...)
	return b
}

func (b *MethodBuilder) WithBody(block BlockStmt) *MethodBuilder {
	b.node.Body = &block
	return b
}

func (b *MethodBuilder) WithStatement(stmt Statement) *MethodBuilder {
	if b.body == nil {
		b.body = &BlockStmt{}
	}
	b.body.Children = append(b.body.Children, stmt)
	return b
}

func (b *MethodBuilder) WithAnnotation(annotation Annotation) *MethodBuilder {
	b.node.Annotations = append(b.node.Annotations, annotation)
	return b
}

func (b *MethodBuilder) Build() MethodNode {
	if b.body != nil && b.node.Body == nil {
		b.node.Body = b.body
	}
	return b.node
}
