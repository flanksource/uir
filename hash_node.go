package uir

import (
	"fmt"
	"sort"
	"strconv"
	"time"
)

func (n nilNode) Hash() string {
	return NewHasher("nil").String()
}

func (n nodeBase) Hash() string {
	h := NewHasher("nodeBase")
	h.AddString("node_type", string(n.GetNodeType()))
	return h.String()
}

func (uir UIR) Hash() string {
	h := NewHasher("uir")
	h.AddNodeList("modules", NodesOf(uir.Modules))
	h.AddNodeList("packages", NodesOf(uir.Packages))
	h.AddNodeList("types", NodesOf(uir.Types))
	h.AddNodeList("records", NodesOf(uir.Records))
	h.AddNodeList("tables", NodesOf(uir.Tables))
	h.AddNodeList("endpoints", NodesOf(uir.Endpoints))
	h.AddNodeList("functions", NodesOf(uir.Functions))
	for i, f := range uir.RawFiles {
		h.AddString(fmt.Sprintf("raw.%d.path", i), f.Path)
		h.AddString(fmt.Sprintf("raw.%d.content", i), f.Content)
	}
	return h.String()
}

func (nl NodeList) Hash() string {
	h := NewHasher("nodeList")
	h.AddNodeList("nodes", []Node(nl))
	return h.String()
}

func (ref NodeRef) Hash() string {
	h := NewHasher("nodeRef")
	h.AddString("symbol", ref.SymbolKey())
	return h.String()
}

func (node UIRNode) Hash() string {
	return node.Value().Hash()
}

func (n ModuleNode) Hash() string {
	h := NewHasher("module")
	h.AddNodeList("packages", NodesOf(n.Packages))
	return h.String()
}

func (p PackageNode) Hash() string {
	h := NewHasher("package")
	h.AddRecordFields("variables", p.Variables)
	h.AddTypes("types", p.Types)
	h.AddRecords("records", p.Records)
	h.AddEndpoints("endpoints", p.Endpoints)
	h.AddMethods("functions", p.Functions)
	h.AddMethods("init_functions", p.InitFunctions)
	return h.String()
}

func (t TypedNode) Hash() string {
	h := NewHasher("type")
	h.AddString("visibility", string(t.Visibility))
	h.AddRecordFields("variables", t.Variables)
	h.AddMethods("methods", t.Methods)
	h.AddTypes("types", t.Types)
	if t.Constructor != nil {
		h.AddString("constructor", t.Constructor.Hash())
	} else {
		h.AddString("constructor", "<nil>")
	}
	if t.Destructor != nil {
		h.AddString("destructor", t.Destructor.Hash())
	} else {
		h.AddString("destructor", "<nil>")
	}
	addTypeParams(h, "type_params", t.TypeParams)
	addTypeReferences(h, "implements", t.Implements)
	h.AddTypeReference("extends", t.Extends)
	h.AddBool("is_abstract", t.IsAbstract)
	return h.String()
}

func (m MethodNode) Hash() string {
	h := NewHasher("method")
	h.AddString("visibility", string(m.Visibility))
	h.AddRecordFields("params", m.Params)
	h.AddRecordFields("returns", m.Returns)
	addErrors(h, "errors", m.Errors)
	if m.Body != nil {
		h.AddString("body", m.Body.Hash())
	} else {
		h.AddString("body", "<nil>")
	}
	h.AddBool("is_async", m.IsAsync)
	h.AddBool("is_generator", m.IsGenerator)
	addTypeParams(h, "type_params", m.TypeParams)
	h.AddTypeReference("return_type", m.ReturnType)
	return h.String()
}

func (f RecordField) Hash() string {
	h := NewHasher("field")
	h.AddString("label", f.Label)
	h.AddString("field_type", string(f.FieldType))
	h.AddTypeReference("type_ref", f.TypeRef)
	h.AddTypedValue("default_value", f.DefaultValue)
	h.AddBool("read_only", f.ReadOnly)
	h.AddBool("write_only", f.WriteOnly)
	h.AddString("visibility", string(f.Visibility))
	h.AddString("validation", hashFieldValidation(f.Validation))
	return h.String()
}

func (r ASTRecord) Hash() string {
	h := NewHasher("record")
	h.AddRecords("extends", r.Extends)
	h.AddString("description", r.Description)
	h.AddString("record_type", string(r.RecordType))
	h.AddRecordFields("fields", r.Fields)
	h.AddStringSlice("examples", r.Examples)
	h.AddString("validation", hashRecordValidation(r.Validation))
	addRecordReferences(h, "references", r.References)
	return h.String()
}

func (e ASTEndpoint) Hash() string {
	h := NewHasher("endpoint")
	h.AddString("method", string(e.Method))
	h.AddString("endpoint_type", string(e.EndpointType))
	h.AddString("input", e.Input.Hash())
	h.AddString("output", e.Output.Hash())
	h.AddStringSlice("examples", e.Examples)
	addErrors(h, "errors", e.Errors)
	return h.String()
}

func hashScopedVariableRef(s ScopedVariableRef) string {
	h := NewHasher("stmt.scoped_variable_ref")
	h.AddString("name", s.Name)
	h.AddTypedValue("default_value", s.DefaultValue)
	h.AddString("identifier", s.SymbolKey())
	return h.String()
}

func hashArguments(args Arguments) string {
	h := NewHasher("arguments")
	h.AddInt("len", len(args))
	for i, arg := range args {
		h.AddStringPtr(fmt.Sprintf("%d.name", i), arg.Name)
		h.AddString(fmt.Sprintf("%d.value", i), HashStatement(arg.Value))
	}
	return h.String()
}

func hashTypeReference(ref TypeReference) string {
	h := NewHasher("type_ref")
	h.AddString("name", ref.Name)
	h.AddString("field_type", string(ref.FieldType))
	addTypeReferences(h, "type_args", ref.TypeArgs)
	addTypeReferences(h, "union", ref.Union)
	addTypeReferences(h, "intersection", ref.Intersection)
	h.AddBool("optional", ref.Optional)
	h.AddStringPtr("literal_value", ref.LiteralValue)
	h.AddBool("is_array", ref.IsArray)
	addTypeReferences(h, "tuple_elements", ref.TupleElements)
	addTypeReferences(h, "function_params", ref.FunctionParams)
	h.AddTypeReference("function_returns", ref.FunctionReturns)
	h.AddString("raw_type", ref.RawType)
	h.AddString("package", ref.Package)
	return h.String()
}

func hashTypedValue(value TypedValue) string {
	h := NewHasher("typed_value")
	if value.Str != nil {
		h.AddString("str", *value.Str)
	}
	if value.Float != nil {
		h.AddFloat64("float", *value.Float)
	}
	if value.Int != nil {
		h.AddString("int", strconv.FormatInt(*value.Int, 10))
	}
	if value.Bool != nil {
		h.AddBool("bool", *value.Bool)
	}
	if value.Date != nil {
		h.AddString("date", value.Date.Format(time.RFC3339Nano))
	}
	if value.Unit != nil {
		h.AddString("unit", value.Unit.String())
	}
	return h.String()
}

func hashValidationValue(value ValidationValue) string {
	h := NewHasher("validation_value")
	h.AddStringPtr("string", value.StringValue)
	if value.NumberValue != nil {
		h.AddFloat64("number", *value.NumberValue)
	}
	if value.BoolValue != nil {
		h.AddBool("bool", *value.BoolValue)
	}
	h.AddInt("array.len", len(value.ArrayValue))
	for i, v := range value.ArrayValue {
		h.AddValidationValue(fmt.Sprintf("array.%d", i), v)
	}
	keys := make([]string, 0, len(value.ObjectValue))
	for key := range value.ObjectValue {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		h.AddValidationValue("object."+key, value.ObjectValue[key])
	}
	h.AddStringPtr("code", value.Code)
	return h.String()
}

func hashFieldValidation(v FieldValidation) string {
	h := NewHasher("field_validation")
	addIntPtr(h, "min_length", v.MinLength)
	addIntPtr(h, "max_length", v.MaxLength)
	addIntPtr(h, "max_items", v.MaxItems)
	addIntPtr(h, "min_items", v.MinItems)
	addIntPtr(h, "min_properties", v.MinProperties)
	addIntPtr(h, "max_properties", v.MaxProperties)
	h.AddBool("unique_items", v.UniqueItems)
	h.AddBool("sorted_items", v.SortedItems)
	h.AddString("regex", v.Regex)
	h.AddStringSlice("enum", v.Enum)
	addFloatPtr(h, "min", v.Min)
	addFloatPtr(h, "min_exclusive", v.MinExclusive)
	addFloatPtr(h, "max_exclusive", v.MaxExclusive)
	addFloatPtr(h, "max", v.Max)
	h.AddStringPtr("min_date", v.MinDate)
	h.AddStringPtr("max_date", v.MaxDate)
	h.AddBool("required", v.Required)
	h.AddBool("unique", v.Unique)
	h.AddBool("non_empty", v.NonEmpty)
	addExpressions(h, "expressions", v.Expressions)
	addValidationRules(h, "rules", v.Rules)
	addGeneratorHints(h, "hints", v.Hints)
	for i, unit := range v.UnitTypes {
		h.AddString(fmt.Sprintf("unit_types.%d", i), string(unit))
	}
	h.AddInt("conditions.len", len(v.Conditions))
	for i, c := range v.Conditions {
		prefix := fmt.Sprintf("conditions.%d", i)
		h.AddString(prefix+".condition", hashExpression(c.Condition))
		h.AddString(prefix+".validation", hashFieldValidation(c.Validation))
	}
	return h.String()
}

func hashRecordValidation(v RecordValidation) string {
	h := NewHasher("record_validation")
	h.AddStringSlice("unique_fields", v.UniqueFields)
	addIntPtr(h, "min_records", v.MinRecords)
	addIntPtr(h, "max_records", v.MaxRecords)
	h.AddStringSlice("any_of", v.AnyOf)
	h.AddStringSlice("one_of", v.OneOf)
	h.AddStringSlice("all_of", v.AllOf)
	h.AddStringSlice("none_of", v.NoneOf)
	addExpressions(h, "expressions", v.Expressions)
	addGeneratorHints(h, "hints", v.Hints)
	h.AddInt("conditions.len", len(v.Conditions))
	for i, c := range v.Conditions {
		prefix := fmt.Sprintf("conditions.%d", i)
		h.AddString(prefix+".condition", hashExpression(c.Condition))
		h.AddString(prefix+".validation", hashRecordValidation(c.Validation))
	}
	return h.String()
}

func hashExpression(e Expression) string {
	h := NewHasher("expression")
	h.AddString("type", string(e.ExpressionType))
	h.AddString("expression", e.Expression)
	return h.String()
}

func addExpressions(h *Hasher, field string, expressions []Expression) {
	h.AddInt(field+".len", len(expressions))
	for i, expr := range expressions {
		h.AddString(fmt.Sprintf("%s.%d", field, i), hashExpression(expr))
	}
}

func addValidationRules(h *Hasher, field string, rules []ValidationRule) {
	h.AddInt(field+".len", len(rules))
	for i, rule := range rules {
		prefix := fmt.Sprintf("%s.%d", field, i)
		h.AddString(prefix+".provider", rule.Provider)
		h.AddString(prefix+".name", rule.Name)
		for j, arg := range rule.Args {
			h.AddValidationValue(fmt.Sprintf("%s.args.%d", prefix, j), arg)
		}
		if rule.Options != nil {
			h.AddString(prefix+".options", hashValidationRuleOptions(*rule.Options))
		}
	}
}

func hashValidationRuleOptions(options ValidationRuleOptions) string {
	h := NewHasher("validation_rule_options")
	h.AddStringPtr("message", options.Message)
	h.AddStringSlice("groups", options.Groups)
	if options.Always != nil {
		h.AddBool("always", *options.Always)
	}
	if options.Each != nil {
		h.AddBool("each", *options.Each)
	}
	keys := make([]string, 0, len(options.Context))
	for key := range options.Context {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		h.AddValidationValue("context."+key, options.Context[key])
	}
	return h.String()
}

func addGeneratorHints(h *Hasher, field string, hints []GeneratorHint) {
	h.AddInt(field+".len", len(hints))
	for i, hint := range hints {
		prefix := fmt.Sprintf("%s.%d", field, i)
		h.AddString(prefix+".language", hint.Language)
		h.AddString(prefix+".framework", hint.Framework)
		keys := make([]string, 0, len(hint.Properties))
		for key := range hint.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			h.AddValidationValue(prefix+".properties."+key, hint.Properties[key])
		}
	}
}

func addTypeReferences(h *Hasher, field string, refs []TypeReference) {
	h.AddInt(field+".len", len(refs))
	for i, ref := range refs {
		h.AddString(fmt.Sprintf("%s.%d", field, i), hashTypeReference(ref))
	}
}

func addTypeParams(h *Hasher, field string, params []TypeParam) {
	h.AddInt(field+".len", len(params))
	for i, param := range params {
		prefix := fmt.Sprintf("%s.%d", field, i)
		h.AddString(prefix+".name", param.Name)
		h.AddTypeReference(prefix+".constraint", param.Constraint)
		h.AddTypeReference(prefix+".default", param.Default)
	}
}

func addErrors(h *Hasher, field string, errors []ASTError) {
	h.AddInt(field+".len", len(errors))
	for i, err := range errors {
		prefix := fmt.Sprintf("%s.%d", field, i)
		h.AddString(prefix+".id", err.ID.ID)
		h.AddString(prefix+".name", err.Name)
		h.AddString(prefix+".error_type", string(err.ErrorType))
		h.AddString(prefix+".description", err.Description)
		h.AddString(prefix+".code", hashTypedValue(err.Code))
	}
}

func addRecordReferences(h *Hasher, field string, refs []RecordReference) {
	h.AddInt(field+".len", len(refs))
	for i, ref := range refs {
		prefix := fmt.Sprintf("%s.%d", field, i)
		h.AddString(prefix+".name", ref.Name)
		h.AddString(prefix+".mapping", ref.Mapping)
		h.AddString(prefix+".type", string(ref.RecordReferenceType))
	}
}

func addIntPtr(h *Hasher, field string, value *int) {
	if value == nil {
		h.AddString(field, "<nil>")
		return
	}
	h.AddInt(field, *value)
}

func addFloatPtr(h *Hasher, field string, value *float64) {
	if value == nil {
		h.AddString(field, "<nil>")
		return
	}
	h.AddFloat64(field, *value)
}
