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

// opaqueLine stands, in a public shape, for the members a consumer outside the package cannot name:
// they still decide comparability and whether an unkeyed literal compiles.
const opaqueLine = "opaque"

// symbolShapes are a declaration's two shape layers. Private is the full declared structure: every
// field with its tag and embedding, every method. Public is what a consumer outside the package
// observes: a struct's exported fields and an interface's exported methods, each with an opaque line
// when unexported members exist. A declaration without members has one shape in both layers.
type symbolShapes struct{ Public, Private string }

func sameShapes(shape string) symbolShapes { return symbolShapes{Public: shape, Private: shape} }

// layerHashes digests the shapes as the layers nest: public_hash is the public shape's digest and
// private_hash digests public_hash and the private shape, so a public change is a private change too.
func layerHashes(shapes symbolShapes) (public, private string) {
	publicDigest := newCanonicalHash(publicHashVersion)
	publicDigest.text(shapes.Public)
	public = publicDigest.sum()
	privateDigest := newCanonicalHash(privateHashVersion)
	privateDigest.text(public)
	privateDigest.text(shapes.Private)
	return public, privateDigest.sum()
}

// shapes renders an object's declared type in the canonical layout, in both layers: one line per
// signature, one gofmt-aligned line per struct field or interface method, and type (and value) for vars
// and consts.
func (resolver *symbolResolver) shapes(object types.Object) (symbolShapes, error) {
	if !validDeclaration(object) {
		return symbolShapes{}, errUnprovenType
	}
	qualifier := func(pkg *types.Package) string {
		if pkg == object.Pkg() {
			return ""
		}
		return pkg.Name()
	}
	switch object := object.(type) {
	case *types.Func:
		return sameShapes(functionShape(object, qualifier)), nil
	case *types.Var:
		if object.IsField() {
			return sameShapes(fieldLine(object, resolver.memberIndex(object.Pkg())[object].tag, qualifier)), nil
		}
		return sameShapes("var " + object.Name() + " " + types.TypeString(object.Type(), qualifier)), nil
	case *types.Const:
		return sameShapes("const " + object.Name() + " " + types.TypeString(object.Type(), qualifier) + " = " + object.Val().ExactString()), nil
	case *types.TypeName:
		return typeShapes(object, qualifier)
	}
	return symbolShapes{}, fmt.Errorf("render shape of %s: unsupported object type %T", object.Name(), object)
}

// apiShape is the API an importer compiles against, encoded by encodeType: types qualified by import
// path, with no receiver, parameter, result, or type parameter names. It mirrors the public layer
// otherwise, so the export shape digests it where public_hash keeps the names symboldiff reports.
func (resolver *symbolResolver) apiShape(object types.Object) (string, error) {
	if !validDeclaration(object) {
		return "", errUnprovenType
	}
	var encoder typeEncoder
	switch object := object.(type) {
	case *types.Func:
		encoder.function(object.Signature())
	case *types.Var:
		encoder.out.WriteString("var ")
		if object.IsField() {
			encoder.field(object, resolver.memberIndex(object.Pkg())[object].tag)
		} else {
			encoder.write(object.Type())
		}
	case *types.Const:
		encoder.out.WriteString("const ")
		encoder.write(object.Type())
		encoder.out.WriteString(" = " + object.Val().ExactString())
	case *types.TypeName:
		if err := encoder.typeName(object); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("encode API of %s: unsupported object type %T", object.Name(), object)
	}
	return encoder.out.String(), nil
}

func (encoder *typeEncoder) function(signature *types.Signature) {
	if receiver := signature.Recv(); receiver != nil {
		encoder.out.WriteString("(")
		encoder.write(receiver.Type())
		encoder.out.WriteString(")")
	}
	encoder.typeParameters(signature.TypeParams())
	encoder.signature(signature)
}

// typeParameters writes each constraint, with the interfaces an inline constraint embeds, which
// iface leaves to the method set.
func (encoder *typeEncoder) typeParameters(parameters *types.TypeParamList) {
	encoder.list("[", parameters.Len(), func(i int) {
		constraint := parameters.At(i).Constraint()
		encoder.write(constraint)
		if iface, inline := types.Unalias(constraint).(*types.Interface); inline {
			for j := range iface.NumEmbeddeds() {
				if embedded := iface.EmbeddedType(j); types.IsInterface(embedded) {
					encoder.out.WriteString("&")
					encoder.write(embedded)
				}
			}
		}
	}, "]")
}

func (encoder *typeEncoder) field(field *types.Var, tag string) {
	if field.Embedded() {
		encoder.out.WriteString("!")
	}
	encoder.write(field.Type())
	if tag != "" {
		encoder.out.WriteString(" " + strconv.Quote(tag))
	}
}

func (encoder *typeEncoder) typeName(object *types.TypeName) error {
	encoder.out.WriteString("type")
	if alias, ok := object.Type().(*types.Alias); ok {
		encoder.typeParameters(alias.TypeParams())
		encoder.out.WriteString(" = ")
		encoder.write(alias.Rhs())
		return nil
	}
	named, ok := object.Type().(*types.Named)
	if !ok {
		return fmt.Errorf("encode API of type %s: %T is not a named type", object.Name(), object.Type())
	}
	encoder.typeParameters(named.TypeParams())
	encoder.out.WriteString(" ")
	switch underlying := named.Underlying().(type) {
	case *types.Struct:
		encoder.publicStruct(underlying)
	case *types.Interface:
		encoder.publicInterface(underlying)
	default:
		encoder.write(underlying)
	}
	return nil
}

// publicStruct writes the exported fields, by name, and an opaque line when unexported ones exist.
func (encoder *typeEncoder) publicStruct(structure *types.Struct) {
	encoder.out.WriteString("struct{")
	hidden := false
	for i := range structure.NumFields() {
		if field := structure.Field(i); field.Exported() {
			encoder.out.WriteString(field.Name() + " ")
			encoder.field(field, structure.Tag(i))
			encoder.out.WriteString(";")
		} else {
			hidden = true
		}
	}
	encoder.opaque(hidden)
}

// publicInterface writes the exported methods, by name, the embedded non-interface types, and an
// opaque line when unexported methods exist.
func (encoder *typeEncoder) publicInterface(iface *types.Interface) {
	encoder.out.WriteString("interface{")
	hidden := false
	for i := range iface.NumMethods() {
		if method := iface.Method(i); method.Exported() {
			encoder.out.WriteString(method.Name())
			encoder.signature(method.Signature())
			encoder.out.WriteString(";")
		} else {
			hidden = true
		}
	}
	for i := range iface.NumEmbeddeds() {
		if embedded := iface.EmbeddedType(i); !types.IsInterface(embedded) {
			encoder.write(embedded)
			encoder.out.WriteString(";")
		}
	}
	encoder.opaque(hidden)
}

func (encoder *typeEncoder) opaque(hidden bool) {
	if hidden {
		encoder.out.WriteString(opaqueLine + ";")
	}
	encoder.out.WriteString("}")
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

func typeShapes(object *types.TypeName, qualifier types.Qualifier) (symbolShapes, error) {
	if object.IsAlias() {
		alias := object.Type().(*types.Alias)
		return sameShapes("type " + object.Name() + typeParameterList(alias.TypeParams(), qualifier) + " = " + types.TypeString(alias.Rhs(), qualifier)), nil
	}
	named, ok := object.Type().(*types.Named)
	if !ok {
		return symbolShapes{}, fmt.Errorf("render shape of type %s: %T is not a named type", object.Name(), object.Type())
	}
	header := "type " + object.Name() + typeParameterList(named.TypeParams(), qualifier)
	var members memberLines
	switch underlying := named.Underlying().(type) {
	case *types.Struct:
		for i := range underlying.NumFields() {
			members.add(fieldLine(underlying.Field(i), underlying.Tag(i), qualifier), underlying.Field(i).Exported())
		}
		return members.format(header + " struct")
	case *types.Interface:
		for i := range underlying.NumMethods() {
			method := underlying.Method(i)
			var line bytes.Buffer
			line.WriteString(method.Name())
			types.WriteSignature(&line, method.Signature(), qualifier)
			members.add(line.String(), method.Exported())
		}
		for i := range underlying.NumEmbeddeds() {
			if embedded := underlying.EmbeddedType(i); !types.IsInterface(embedded) {
				members.add(types.TypeString(embedded, qualifier), true)
			}
		}
		return members.format(header + " interface")
	default:
		return sameShapes(header + " " + types.TypeString(underlying, qualifier)), nil
	}
}

// memberLines collects a struct's or interface's member lines for both layers.
type memberLines struct {
	public, private []string
	hidden          bool
}

func (members *memberLines) add(line string, exported bool) {
	members.private = append(members.private, line)
	if exported {
		members.public = append(members.public, line)
	} else {
		members.hidden = true
	}
}

func (members memberLines) format(header string) (symbolShapes, error) {
	public := members.public
	if members.hidden {
		public = append(public, opaqueLine)
	}
	publicShape, err := formatBlock(header, public)
	if err != nil {
		return symbolShapes{}, err
	}
	privateShape, err := formatBlock(header, members.private)
	return symbolShapes{Public: publicShape, Private: privateShape}, err
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
