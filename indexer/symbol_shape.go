package indexer

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"go/types"
	"strconv"
	"strings"
)

// errUnprovenType marks a declaration whose type involves an invalid type: its facts are not proven.
var errUnprovenType = errors.New("declaration involves an invalid type")

// typeEncoder writes a type's identity: fully qualified named types, composite types spelled out
// recursively, and type parameters by ordinal. It never descends into a named type's definition.
type typeEncoder struct {
	out     strings.Builder
	invalid bool
}

func encodeType(typ types.Type) (string, bool) {
	var encoder typeEncoder
	encoder.write(typ)
	return encoder.out.String(), !encoder.invalid
}

func (encoder *typeEncoder) write(typ types.Type) {
	switch typ := typ.(type) {
	case *types.Basic:
		encoder.basic(typ)
	case *types.Alias:
		encoder.write(types.Unalias(typ))
	case *types.Named:
		if typ.Obj().Pkg() != nil {
			encoder.out.WriteString(typ.Obj().Pkg().Path() + ".")
		}
		encoder.out.WriteString(typ.Obj().Name())
		encoder.list("[", typ.TypeArgs().Len(), func(i int) { encoder.write(typ.TypeArgs().At(i)) }, "]")
	case *types.TypeParam:
		encoder.out.WriteString("$" + strconv.Itoa(typ.Index()))
	case *types.Pointer:
		encoder.out.WriteString("*")
		encoder.write(typ.Elem())
	case *types.Slice:
		encoder.out.WriteString("[]")
		encoder.write(typ.Elem())
	case *types.Array:
		encoder.out.WriteString("[" + strconv.FormatInt(typ.Len(), 10) + "]")
		encoder.write(typ.Elem())
	case *types.Map:
		encoder.out.WriteString("map[")
		encoder.write(typ.Key())
		encoder.out.WriteString("]")
		encoder.write(typ.Elem())
	case *types.Chan:
		encoder.out.WriteString([...]string{types.SendRecv: "chan(", types.SendOnly: "chan<-(", types.RecvOnly: "<-chan("}[typ.Dir()])
		encoder.write(typ.Elem())
		encoder.out.WriteString(")")
	case *types.Signature:
		encoder.signature(typ)
	case *types.Struct:
		encoder.structure(typ)
	case *types.Interface:
		encoder.iface(typ)
	case *types.Union:
		encoder.list("", typ.Len(), func(i int) {
			if typ.Term(i).Tilde() {
				encoder.out.WriteString("~")
			}
			encoder.write(typ.Term(i).Type())
		}, "")
	default:
		encoder.invalid = true
		fmt.Fprintf(&encoder.out, "unsupported(%T)", typ)
	}
}

func (encoder *typeEncoder) basic(typ *types.Basic) {
	switch typ.Kind() {
	case types.Invalid:
		encoder.invalid = true
		encoder.out.WriteString("invalid")
	case types.UnsafePointer:
		encoder.out.WriteString("unsafe.Pointer")
	default:
		encoder.out.WriteString(typ.Name())
	}
}

func (encoder *typeEncoder) list(open string, count int, item func(int), close string) {
	if count == 0 && open == "[" {
		return
	}
	encoder.out.WriteString(open)
	for i := range count {
		if i > 0 {
			encoder.out.WriteString(",")
		}
		item(i)
	}
	encoder.out.WriteString(close)
}

func (encoder *typeEncoder) tuple(tuple *types.Tuple, variadic bool) {
	encoder.list("(", tuple.Len(), func(i int) {
		parameter := tuple.At(i).Type()
		if slice, ok := parameter.(*types.Slice); ok && variadic && i == tuple.Len()-1 {
			encoder.out.WriteString("...")
			parameter = slice.Elem()
		}
		encoder.write(parameter)
	}, ")")
}

func (encoder *typeEncoder) signature(signature *types.Signature) {
	encoder.out.WriteString("func")
	encoder.tuple(signature.Params(), signature.Variadic())
	encoder.tuple(signature.Results(), false)
}

func (encoder *typeEncoder) structure(structure *types.Struct) {
	encoder.out.WriteString("struct{")
	for i := range structure.NumFields() {
		field := structure.Field(i)
		if field.Embedded() {
			encoder.out.WriteString("!")
		}
		encoder.out.WriteString(field.Name() + " ")
		encoder.write(field.Type())
		if tag := structure.Tag(i); tag != "" {
			encoder.out.WriteString(" " + strconv.Quote(tag))
		}
		encoder.out.WriteString(";")
	}
	encoder.out.WriteString("}")
}

func (encoder *typeEncoder) iface(iface *types.Interface) {
	encoder.out.WriteString("interface{")
	for i := range iface.NumMethods() {
		method := iface.Method(i)
		encoder.out.WriteString(method.Name())
		encoder.signature(method.Signature())
		encoder.out.WriteString(";")
	}
	for i := range iface.NumEmbeddeds() {
		if embedded := iface.EmbeddedType(i); !types.IsInterface(embedded) {
			encoder.write(embedded)
			encoder.out.WriteString(";")
		}
	}
	encoder.out.WriteString("}")
}

func shapeHash(shape string) string {
	digest := newCanonicalHash(shapeHashVersion)
	digest.text(shape)
	return digest.sum()
}

// shape renders an object's declared type in the canonical layout: one line per signature, one
// gofmt-aligned line per struct field or interface method, and type (and value) for vars and consts.
func (resolver *symbolResolver) shape(object types.Object) (string, error) {
	if !validDeclaration(object) {
		return "", errUnprovenType
	}
	qualifier := func(pkg *types.Package) string {
		if pkg == object.Pkg() {
			return ""
		}
		return pkg.Name()
	}
	switch object := object.(type) {
	case *types.Func:
		return functionShape(object, qualifier), nil
	case *types.Var:
		if object.IsField() {
			return fieldLine(object, resolver.memberIndex(object.Pkg())[object].tag, qualifier), nil
		}
		return "var " + object.Name() + " " + types.TypeString(object.Type(), qualifier), nil
	case *types.Const:
		return "const " + object.Name() + " " + types.TypeString(object.Type(), qualifier) + " = " + object.Val().ExactString(), nil
	case *types.TypeName:
		return typeShape(object, qualifier)
	}
	return "", fmt.Errorf("render shape of %s: unsupported object type %T", object.Name(), object)
}

func validDeclaration(object types.Object) bool {
	if _, valid := encodeType(object.Type()); !valid {
		return false
	}
	if typeName, ok := object.(*types.TypeName); ok && !typeName.IsAlias() {
		_, valid := encodeType(typeName.Type().Underlying())
		return valid
	}
	return true
}

func functionShape(function *types.Func, qualifier types.Qualifier) string {
	var shape bytes.Buffer
	shape.WriteString("func ")
	signature := function.Signature()
	if receiver := signature.Recv(); receiver != nil {
		shape.WriteString("(")
		if receiver.Name() != "" && receiver.Name() != "_" {
			shape.WriteString(receiver.Name() + " ")
		}
		shape.WriteString(types.TypeString(receiver.Type(), qualifier) + ") ")
	}
	shape.WriteString(function.Name())
	types.WriteSignature(&shape, signature, qualifier)
	return shape.String()
}

func fieldLine(field *types.Var, tag string, qualifier types.Qualifier) string {
	line := types.TypeString(field.Type(), qualifier)
	if !field.Embedded() {
		line = field.Name() + " " + line
	}
	if tag != "" {
		if strconv.CanBackquote(tag) {
			line += " `" + tag + "`"
		} else {
			line += " " + strconv.Quote(tag)
		}
	}
	return line
}

func typeShape(object *types.TypeName, qualifier types.Qualifier) (string, error) {
	if object.IsAlias() {
		alias := object.Type().(*types.Alias)
		return "type " + object.Name() + typeParameterList(alias.TypeParams(), qualifier) + " = " + types.TypeString(alias.Rhs(), qualifier), nil
	}
	named, ok := object.Type().(*types.Named)
	if !ok {
		return "", fmt.Errorf("render shape of type %s: %T is not a named type", object.Name(), object.Type())
	}
	header := "type " + object.Name() + typeParameterList(named.TypeParams(), qualifier)
	var lines []string
	switch underlying := named.Underlying().(type) {
	case *types.Struct:
		for i := range underlying.NumFields() {
			lines = append(lines, fieldLine(underlying.Field(i), underlying.Tag(i), qualifier))
		}
		return formatBlock(header+" struct", lines)
	case *types.Interface:
		for i := range underlying.NumMethods() {
			method := underlying.Method(i)
			var line bytes.Buffer
			line.WriteString(method.Name())
			types.WriteSignature(&line, method.Signature(), qualifier)
			lines = append(lines, line.String())
		}
		for i := range underlying.NumEmbeddeds() {
			if embedded := underlying.EmbeddedType(i); !types.IsInterface(embedded) {
				lines = append(lines, types.TypeString(embedded, qualifier))
			}
		}
		return formatBlock(header+" interface", lines)
	default:
		return header + " " + types.TypeString(underlying, qualifier), nil
	}
}

func typeParameterList(parameters *types.TypeParamList, qualifier types.Qualifier) string {
	if parameters.Len() == 0 {
		return ""
	}
	rendered := make([]string, parameters.Len())
	for i := range parameters.Len() {
		parameter := parameters.At(i)
		rendered[i] = parameter.Obj().Name() + " " + types.TypeString(parameter.Constraint(), qualifier)
	}
	return "[" + strings.Join(rendered, ", ") + "]"
}

// formatBlock lays a struct or interface out one member per line, aligned by gofmt.
func formatBlock(header string, lines []string) (string, error) {
	source := header + " {\n"
	for _, line := range lines {
		source += "\t" + line + "\n"
	}
	source += "}\n"
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return "", fmt.Errorf("format %q: %w", source, err)
	}
	return strings.TrimSuffix(string(formatted), "\n"), nil
}
