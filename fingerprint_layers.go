package uir

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func moduleLayers(n ModuleNode) layers {
	var c composition
	c.add(NodesOf(n.Packages), unordered, false)
	return c.layers(ShapeKind(n), "", "")
}

func packageLayers(p PackageNode) layers {
	var c composition
	c.add(NodesOf(p.Types), unordered, false)
	c.add(NodesOf(p.Records), unordered, false)
	c.add(NodesOf(p.Tables), unordered, false)
	c.add(NodesOf(p.Endpoints), unordered, false)
	c.add(NodesOf(p.Functions), unordered, false)
	c.add(NodesOf(p.Variables), unordered, false)
	c.add(NodesOf(p.InitFunctions), unordered, true)
	return c.layers(ShapeKind(p), "", "")
}

func typedLayers(t TypedNode) layers {
	var c composition
	c.add(NodesOf(t.Variables), declaredOrder, false)
	for _, special := range []*MethodNode{t.Constructor, t.Destructor} {
		if special != nil {
			c.add([]Node{*special}, declaredOrder, false)
		}
	}
	c.add(NodesOf(t.Methods), unordered, false)
	c.add(NodesOf(t.Types), unordered, false)
	c.opaque()
	facts := visibilityText(t.Visibility) + typeParamsText(t.TypeParams)
	if t.Extends != nil {
		facts += " extends " + typeRefText(*t.Extends)
	}
	if len(t.Implements) > 0 {
		facts += " implements " + typeRefsText(t.Implements, ", ")
	}
	if t.IsAbstract {
		facts += " abstract"
	}
	return c.layers(ShapeKind(t), facts, "")
}

func methodLayers(m MethodNode) layers {
	content := ""
	if m.Body != nil {
		content = m.Body.Hash()
	}
	return leafLayers(ShapeKind(m), methodFacts(m, LayerPublic),
		methodFacts(m, LayerPrivate)+declarationText(m.Properties)+sqlDependenciesText(m.SQLDependencies), content)
}

// methodFacts renders a signature; parameters carry their private facts (such as
// defaults) only in the private layer.
func methodFacts(m MethodNode, layer Layer) string {
	facts := visibilityText(m.Visibility) + typeParamsText(m.TypeParams) + " (" + paramsText(m.Params, layer) + ")"
	if len(m.Returns) > 0 {
		facts += " (" + paramsText(m.Returns, layer) + ")"
	}
	if m.ReturnType != nil {
		facts += " " + typeRefText(*m.ReturnType)
	}
	if len(m.Errors) > 0 {
		facts += " throws " + errorsText(m.Errors)
	}
	return facts + flagText(m.IsAsync, "async") + flagText(m.IsGenerator, "generator")
}

func paramsText(params []RecordField, layer Layer) string {
	text := make([]string, len(params))
	for i, param := range params {
		text[i] = param.Field + fieldFacts(param, layer)
	}
	return strings.Join(text, ", ")
}

// declarationText renders a trigger's event and timing.
func declarationText(properties map[string]any) string {
	text := ""
	for _, key := range []string{TriggerEventProperty, TriggerTimingProperty, TriggerDisabledProperty} {
		if value, ok := properties[key]; ok {
			text += " " + key + "=" + fmt.Sprint(value)
		}
	}
	return text
}

func fieldLayers(f RecordField) layers {
	return leafLayers(ShapeKind(f), fieldFacts(f, LayerPublic), fieldFacts(f, LayerPrivate), "")
}

func fieldFacts(f RecordField, layer Layer) string {
	facts := sqlTypeText(f.SQLType, f.FieldType)
	if f.TypeRef != nil {
		facts += " " + typeRefText(*f.TypeRef)
	}
	facts += flagText(f.ReadOnly, "readonly") + flagText(f.WriteOnly, "writeonly") + visibilityText(f.Visibility)
	if layer != LayerPrivate {
		return facts
	}
	if f.DefaultValue != nil {
		facts += " default=" + canonicalJSON(*f.DefaultValue)
	}
	if f.Label != "" {
		facts += " label=" + strconv.Quote(f.Label)
	}
	if validation := canonicalJSON(f.Validation); validation != "{}" {
		facts += " validation=" + validation
	}
	return facts
}

func recordLayers(r ASTRecord) layers {
	var c composition
	c.add(NodesOf(r.Fields), declaredOrder, false)
	c.add(NodesOf(r.Extends), declaredOrder, false)
	c.opaque()
	for _, ref := range r.References {
		c.private = append(c.private, "reference "+ref.Name+" "+string(ref.RecordReferenceType)+" "+ref.Mapping)
	}
	if validation := canonicalJSON(r.Validation); validation != "{}" {
		c.private = append(c.private, "validation "+validation)
	}
	facts := ""
	if r.RecordType != "" {
		facts = " " + string(r.RecordType)
	}
	return c.layers(ShapeKind(r), facts, "")
}

// endpointLayers composes the input and output records under their role, since
// both are what a caller of the endpoint observes.
func endpointLayers(e ASTEndpoint) layers {
	var c composition
	for _, role := range []struct {
		name   string
		record ASTRecord
	}{{"input", e.Input}, {"output", e.Output}} {
		m := memberOf(role.record)
		m.name = role.name
		c.public = append(c.public, m.line(LayerPublic))
		c.private = append(c.private, m.line(LayerPrivate))
		if m.prints.Content != "" {
			c.content = append(c.content, role.name+" "+m.prints.Content)
		}
	}
	facts := ""
	for _, fact := range []string{string(e.Method), string(e.EndpointType)} {
		if fact != "" {
			facts += " " + fact
		}
	}
	if len(e.Errors) > 0 {
		facts += " throws " + errorsText(e.Errors)
	}
	return c.layers(ShapeKind(e), facts, "")
}

func refLayers(ref NodeRef) layers {
	return leafLayers(NodeTypeRef, " "+ref.SymbolKey(), " "+ref.SymbolKey(), "")
}

func visibilityText(visibility Visibility) string {
	if visibility == VisibilityNA {
		return ""
	}
	return " " + string(visibility)
}

func flagText(set bool, flag string) string {
	if !set {
		return ""
	}
	return " " + flag
}

func sqlTypeText(sqlType string, fieldType RecordFieldType) string {
	text := ""
	if sqlType != "" {
		text += " " + sqlType
	}
	if fieldType != "" {
		text += " (" + string(fieldType) + ")"
	}
	return text
}

func typeParamsText(params []TypeParam) string {
	if len(params) == 0 {
		return ""
	}
	text := make([]string, len(params))
	for i, param := range params {
		text[i] = param.Name
		if param.Constraint != nil {
			text[i] += " " + typeRefText(*param.Constraint)
		}
		if param.Default != nil {
			text[i] += " = " + typeRefText(*param.Default)
		}
	}
	return " [" + strings.Join(text, ", ") + "]"
}

func errorsText(errors []ASTError) string {
	text := make([]string, len(errors))
	for i, err := range errors {
		text[i] = err.Name + ":" + string(err.ErrorType)
		if !err.Code.IsEmpty() {
			text[i] += "=" + canonicalJSON(err.Code)
		}
	}
	return strings.Join(text, ", ")
}

func typeRefsText(refs []TypeReference, sep string) string {
	text := make([]string, len(refs))
	for i, ref := range refs {
		text[i] = typeRefText(ref)
	}
	return strings.Join(text, sep)
}

func typeRefText(ref TypeReference) string {
	text := ref.Name
	if ref.Package != "" {
		text = ref.Package + "." + text
	}
	if ref.FieldType != "" {
		text += ":" + string(ref.FieldType)
	}
	if len(ref.TypeArgs) > 0 {
		text += "<" + typeRefsText(ref.TypeArgs, ", ") + ">"
	}
	if len(ref.Union) > 0 {
		text += "(" + typeRefsText(ref.Union, " | ") + ")"
	}
	if len(ref.Intersection) > 0 {
		text += "(" + typeRefsText(ref.Intersection, " & ") + ")"
	}
	if len(ref.TupleElements) > 0 {
		text += "[" + typeRefsText(ref.TupleElements, ", ") + "]"
	}
	if len(ref.FunctionParams) > 0 || ref.FunctionReturns != nil {
		text += "func(" + typeRefsText(ref.FunctionParams, ", ") + ")"
		if ref.FunctionReturns != nil {
			text += " " + typeRefText(*ref.FunctionReturns)
		}
	}
	if ref.LiteralValue != nil {
		text += strconv.Quote(*ref.LiteralValue)
	}
	if ref.RawType != "" {
		text += "raw(" + strconv.Quote(ref.RawType) + ")"
	}
	return text + flagText(ref.IsArray, "[]") + flagText(ref.Optional, "?")
}

// canonicalJSON renders a value whose encoding is deterministic (structs, and
// maps, whose keys encoding/json sorts). A value that cannot encode is a model
// invariant broken, not a hash input to skip.
func canonicalJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("uir.Fingerprint: cannot encode %T: %v", value, err))
	}
	return string(data)
}
