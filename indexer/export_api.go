package indexer

import (
	"go/types"
	"sort"
	"strings"
)

// apiWalker collects the packages whose named types a package's exported API reaches. It descends
// into the package's own named types, exported or not, because an importer reaches an unexported type
// through an exported signature, and every embedded field, because promotion exposes its selectors;
// another package's named type is recorded and not descended into, since its own export shape covers it.
type apiWalker struct {
	own        *types.Package
	seen       map[types.Type]bool
	referenced map[*types.Package]bool
}

// apiReferences is every other package, sorted by path, whose named types pkg's exported API names.
func apiReferences(pkg *types.Package) []*types.Package {
	walker := apiWalker{own: pkg, seen: map[types.Type]bool{}, referenced: map[*types.Package]bool{}}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		if object := scope.Lookup(name); object.Exported() {
			walker.walk(object.Type())
		}
	}
	referenced := make([]*types.Package, 0, len(walker.referenced))
	for imported := range walker.referenced {
		referenced = append(referenced, imported)
	}
	sort.Slice(referenced, func(i, j int) bool { return referenced[i].Path() < referenced[j].Path() })
	return referenced
}

func (walker *apiWalker) walk(typ types.Type) {
	switch typ := typ.(type) {
	case *types.Named:
		walker.named(typ)
	case *types.Alias:
		walker.walk(types.Unalias(typ))
	case *types.Pointer:
		walker.walk(typ.Elem())
	case *types.Slice:
		walker.walk(typ.Elem())
	case *types.Array:
		walker.walk(typ.Elem())
	case *types.Chan:
		walker.walk(typ.Elem())
	case *types.Map:
		walker.walk(typ.Key())
		walker.walk(typ.Elem())
	case *types.Signature:
		walker.typeParameters(typ.TypeParams())
		walker.tuple(typ.Params())
		walker.tuple(typ.Results())
	case *types.Struct:
		for i := range typ.NumFields() {
			if field := typ.Field(i); field.Exported() || field.Embedded() {
				walker.walk(field.Type())
			}
		}
	case *types.Interface:
		walker.iface(typ)
	case *types.Union:
		for i := range typ.Len() {
			walker.walk(typ.Term(i).Type())
		}
	case *types.TypeParam:
		if !walker.seen[typ] {
			walker.seen[typ] = true
			walker.walk(typ.Constraint())
		}
	}
}

// named records another package's type, or descends once into one of the package's own: its
// underlying type, type parameters, and exported methods.
func (walker *apiWalker) named(named *types.Named) {
	for i := range named.TypeArgs().Len() {
		walker.walk(named.TypeArgs().At(i))
	}
	pkg := named.Obj().Pkg()
	if pkg == nil {
		return
	}
	if pkg != walker.own {
		walker.referenced[pkg] = true
		return
	}
	origin := named.Origin()
	if walker.seen[origin] {
		return
	}
	walker.seen[origin] = true
	walker.typeParameters(origin.TypeParams())
	walker.walk(origin.Underlying())
	for i := range origin.NumMethods() {
		if method := origin.Method(i); method.Exported() {
			walker.walk(method.Type())
		}
	}
}

func (walker *apiWalker) iface(iface *types.Interface) {
	for i := range iface.NumExplicitMethods() {
		if method := iface.ExplicitMethod(i); method.Exported() {
			walker.walk(method.Type())
		}
	}
	for i := range iface.NumEmbeddeds() {
		walker.walk(iface.EmbeddedType(i))
	}
}

func (walker *apiWalker) tuple(tuple *types.Tuple) {
	for i := range tuple.Len() {
		walker.walk(tuple.At(i).Type())
	}
}

func (walker *apiWalker) typeParameters(parameters *types.TypeParamList) {
	for i := range parameters.Len() {
		walker.walk(parameters.At(i))
	}
}

// selectorPairs is (type id, promoted selectors) for every exported defined non-interface type of the
// package that promotes an exported method or field through an embedded field. The API shape names a
// type's own fields and methods; what embedding promotes, possibly through an unexported field or
// from another package, is part of what an importer can select and which interfaces it satisfies.
func (shapes *exportShapes) selectorPairs(pkg *types.Package) ([][2]string, error) {
	var pairs [][2]string
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		typeName, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !typeName.Exported() || typeName.IsAlias() {
			continue
		}
		named, ok := typeName.Type().(*types.Named)
		if !ok || types.IsInterface(named) {
			continue
		}
		selectors := promotedSelectors(named)
		if selectors == "" {
			continue
		}
		resolved, err := shapes.resolver.resolve(typeName)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, [2]string{resolved.ID, "selectors " + selectors})
	}
	return pairs, nil
}

// promotedSelectors encodes the exported methods and fields a defined type gets only through
// embedding: each promoted method with its declaring receiver, and each promoted field with its
// embedding path, both encoded by the API encoding.
func promotedSelectors(named *types.Named) string {
	var encoder typeEncoder
	methods := types.NewMethodSet(types.NewPointer(named))
	for i := range methods.Len() {
		selection := methods.At(i)
		if len(selection.Index()) < 2 || !selection.Obj().Exported() {
			continue
		}
		encoder.out.WriteString("method " + selection.Obj().Name())
		encoder.function(selection.Obj().(*types.Func).Signature())
		encoder.out.WriteString(";")
	}
	if structure, ok := named.Underlying().(*types.Struct); ok {
		encoder.promotedFields(structure, nil, map[*types.Named]bool{named.Origin(): true})
	}
	return encoder.out.String()
}

// promotedFields writes the exported fields reached through the embedded fields of structure; path is
// the embedding chain to it. A type embedded twice on one chain is not descended into again.
func (encoder *typeEncoder) promotedFields(structure *types.Struct, path []string, visited map[*types.Named]bool) {
	for i := range structure.NumFields() {
		field := structure.Field(i)
		if len(path) > 0 && field.Exported() {
			encoder.out.WriteString("field " + strings.Join(append(path, field.Name()), "."))
			encoder.field(field, structure.Tag(i))
			encoder.out.WriteString(";")
		}
		if !field.Embedded() {
			continue
		}
		embedded := types.Unalias(field.Type())
		if pointer, ok := embedded.(*types.Pointer); ok {
			embedded = types.Unalias(pointer.Elem())
		}
		named, ok := embedded.(*types.Named)
		if !ok || visited[named.Origin()] {
			continue
		}
		if inner, ok := named.Underlying().(*types.Struct); ok {
			visited[named.Origin()] = true
			encoder.promotedFields(inner, append(append([]string(nil), path...), field.Name()), visited)
			delete(visited, named.Origin())
		}
	}
}
