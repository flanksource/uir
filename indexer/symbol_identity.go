package indexer

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"strconv"
	"strings"

	"github.com/flanksource/uir/storage"
)

const (
	symbolIdentityDomain  = "uir-symbol"
	symbolIdentityVersion = 1
)

// Notes on occurrences whose target is not a canonical symbol.
const (
	noteLocal            = "local binding"
	noteTypeParameter    = "type parameter"
	noteLabel            = "label"
	noteBlank            = "blank identifier"
	noteAnonymousMember  = "member of a local or anonymous type"
	noteUnresolved       = "unresolved"
	noteUnresolvedImport = "unresolved import"
	noteUnproven         = "unproven: involves an invalid type"
)

// symbolIdentity is the input to a canonical symbol id; see docs/symbol-index-storage.md.
type symbolIdentity struct {
	ModuleKey      string
	PackagePath    string
	Kind           string
	OwnerID        string
	Name           string
	ParameterTypes []string
}

// canonicalKey is the versioned, length-delimited encoding the id digests: every field is written
// as <byte length>:<bytes>, so no two field sequences share an encoding.
func (identity symbolIdentity) canonicalKey() string {
	var key strings.Builder
	field := func(value string) {
		key.WriteString(strconv.Itoa(len(value)))
		key.WriteByte(':')
		key.WriteString(value)
		key.WriteByte(',')
	}
	field(symbolIdentityDomain)
	field(strconv.Itoa(symbolIdentityVersion))
	field(identity.ModuleKey)
	field(identity.PackagePath)
	field(identity.Kind)
	field(identity.OwnerID)
	field(identity.Name)
	field(strconv.Itoa(len(identity.ParameterTypes)))
	for _, parameter := range identity.ParameterTypes {
		field(parameter)
	}
	return key.String()
}

func (identity symbolIdentity) id() string { return hashBytes([]byte(identity.canonicalKey())) }

type packageClass int

const (
	standardPackage packageClass = iota + 1
	workspacePackage
	modulePackage
	unresolvedPackage
)

// packageOrigin is where a loaded package comes from, which decides its module key and export shape.
type packageOrigin struct {
	Class      packageClass
	ModulePath string
	Version    string
}

func (origin packageOrigin) moduleKey() string {
	if origin.Class == standardPackage {
		return "std"
	}
	return origin.ModulePath
}

// resolvedSymbol is an object's canonical symbol id, or the note explaining why it has none.
type resolvedSymbol struct {
	ID   string
	Note string
}

// memberOwner is the declaration that owns a field or an anonymous interface's method.
type memberOwner struct {
	owner types.Object
	tag   string
}

// symbolResolver maps type-checker objects to canonical symbols and collects their rows.
type symbolResolver struct {
	origins  map[*types.Package]packageOrigin
	members  map[*types.Package]map[types.Object]memberOwner
	resolved map[types.Object]resolvedSymbol
	rows     map[string]storage.Symbol
}

func newSymbolResolver(origins map[*types.Package]packageOrigin) *symbolResolver {
	return &symbolResolver{
		origins: origins, members: map[*types.Package]map[types.Object]memberOwner{},
		resolved: map[types.Object]resolvedSymbol{}, rows: map[string]storage.Symbol{},
	}
}

func noted(note string) (resolvedSymbol, error) { return resolvedSymbol{Note: note}, nil }

func (resolver *symbolResolver) resolve(object types.Object) (resolvedSymbol, error) {
	switch typed := object.(type) {
	case *types.Var:
		object = typed.Origin()
	case *types.Func:
		object = typed.Origin()
	}
	if resolved, found := resolver.resolved[object]; found {
		return resolved, nil
	}
	resolved, err := resolver.resolveObject(object)
	if err != nil {
		return resolvedSymbol{}, err
	}
	resolver.resolved[object] = resolved
	return resolved, nil
}

func (resolver *symbolResolver) resolveObject(object types.Object) (resolvedSymbol, error) {
	if object.Name() == "_" {
		return noted(noteBlank)
	}
	if object.Pkg() == nil {
		return resolver.builtin(object)
	}
	switch object := object.(type) {
	case *types.PkgName:
		return resolver.packageSymbol(object.Imported())
	case *types.Label:
		return noted(noteLabel)
	case *types.Builtin:
		return resolver.declare(object, "func", "", nil)
	case *types.TypeName:
		if _, parameter := object.Type().(*types.TypeParam); parameter {
			return noted(noteTypeParameter)
		}
		return resolver.packageLevel(object, "type")
	case *types.Const:
		return resolver.packageLevel(object, "const")
	case *types.Var:
		if object.IsField() {
			return resolver.member(object, "field", nil)
		}
		return resolver.packageLevel(object, "var")
	case *types.Func:
		parameters, valid := parameterTypes(object.Signature())
		if !valid {
			return noted(noteUnproven)
		}
		if object.Signature().Recv() != nil {
			return resolver.method(object, parameters)
		}
		return resolver.declare(object, "func", "", parameters)
	}
	return resolvedSymbol{}, fmt.Errorf("resolve %s: unsupported object type %T", object.Name(), object)
}

func (resolver *symbolResolver) packageLevel(object types.Object, kind string) (resolvedSymbol, error) {
	if object.Parent() != object.Pkg().Scope() {
		return noted(noteLocal)
	}
	return resolver.declare(object, kind, "", nil)
}

// builtin records a universe-scope object; the error interface's Error method is owned by error.
func (resolver *symbolResolver) builtin(object types.Object) (resolvedSymbol, error) {
	identity := symbolIdentity{Kind: "builtin", Name: object.Name()}
	if function, ok := object.(*types.Func); ok && function.Signature().Recv() != nil {
		owner, err := resolver.resolve(types.Universe.Lookup("error"))
		if err != nil {
			return resolvedSymbol{}, err
		}
		identity.OwnerID = owner.ID
	}
	return resolver.record(identity, "exported")
}

func (resolver *symbolResolver) packageSymbol(pkg *types.Package) (resolvedSymbol, error) {
	origin, err := resolver.origin(pkg)
	if err != nil {
		return resolvedSymbol{}, err
	}
	if origin.Class == unresolvedPackage {
		return noted(noteUnresolvedImport)
	}
	return resolver.record(symbolIdentity{ModuleKey: origin.moduleKey(), PackagePath: pkg.Path(), Kind: "package", Name: pkg.Name()}, "exported")
}

func (resolver *symbolResolver) method(function *types.Func, parameters []string) (resolvedSymbol, error) {
	receiver := function.Signature().Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	if named, ok := types.Unalias(receiver).(*types.Named); ok {
		owner, err := resolver.resolve(named.Origin().Obj())
		if err != nil || owner.ID == "" {
			return owner, err
		}
		return resolver.declare(function, "method", owner.ID, parameters)
	}
	return resolver.member(function, "method", parameters)
}

// member resolves a field or an anonymous interface's method through its package's member index.
func (resolver *symbolResolver) member(object types.Object, kind string, parameters []string) (resolvedSymbol, error) {
	entry, found := resolver.memberIndex(object.Pkg())[object]
	if !found {
		return noted(noteAnonymousMember)
	}
	owner, err := resolver.resolve(entry.owner)
	if err != nil || owner.ID == "" {
		return owner, err
	}
	return resolver.declare(object, kind, owner.ID, parameters)
}

func (resolver *symbolResolver) declare(object types.Object, kind, ownerID string, parameters []string) (resolvedSymbol, error) {
	origin, err := resolver.origin(object.Pkg())
	if err != nil {
		return resolvedSymbol{}, err
	}
	if origin.Class == unresolvedPackage {
		return noted(noteUnresolvedImport)
	}
	visibility := "internal"
	if ast.IsExported(object.Name()) && (ownerID == "" || resolver.rows[ownerID].Visibility == "exported") {
		visibility = "exported"
	}
	return resolver.record(symbolIdentity{
		ModuleKey: origin.moduleKey(), PackagePath: object.Pkg().Path(), Kind: kind,
		OwnerID: ownerID, Name: object.Name(), ParameterTypes: parameters,
	}, visibility)
}

func (resolver *symbolResolver) origin(pkg *types.Package) (packageOrigin, error) {
	if pkg == types.Unsafe {
		return packageOrigin{Class: standardPackage}, nil
	}
	origin, found := resolver.origins[pkg]
	if !found {
		return packageOrigin{}, fmt.Errorf("package %q is not part of the loaded package graph", pkg.Path())
	}
	return origin, nil
}

func (resolver *symbolResolver) record(identity symbolIdentity, visibility string) (resolvedSymbol, error) {
	id, key := identity.id(), identity.canonicalKey()
	if existing, found := resolver.rows[id]; found {
		if existing.CanonicalKey != key {
			return resolvedSymbol{}, fmt.Errorf("symbol id %s digests %q and %q", id, existing.CanonicalKey, key)
		}
		return resolvedSymbol{ID: id}, nil
	}
	parameters := identity.ParameterTypes
	if parameters == nil {
		parameters = []string{}
	}
	encoded, err := json.Marshal(parameters)
	if err != nil {
		return resolvedSymbol{}, fmt.Errorf("encode parameter types of %s: %w", identity.Name, err)
	}
	row := storage.Symbol{
		ID: id, IdentityVersion: symbolIdentityVersion, CanonicalKey: key, ModuleKey: identity.ModuleKey,
		PackagePath: identity.PackagePath, Kind: identity.Kind, Name: identity.Name,
		SearchName: storage.SearchName(identity.Name), Visibility: visibility, ParameterTypes: storage.JSON(encoded),
	}
	if identity.OwnerID != "" {
		owner := identity.OwnerID
		row.OwnerID = &owner
	}
	resolver.rows[id] = row
	return resolvedSymbol{ID: id}, nil
}

// memberIndex maps every field and anonymous-interface method reachable from a package-level
// declaration to its owner: a named type, an alias of a literal type, a variable of a literal type,
// or, for a nested literal struct, the field whose type it is.
func (resolver *symbolResolver) memberIndex(pkg *types.Package) map[types.Object]memberOwner {
	if index, built := resolver.members[pkg]; built {
		return index
	}
	index := map[types.Object]memberOwner{}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		switch object := scope.Lookup(name).(type) {
		case *types.TypeName:
			if object.IsAlias() {
				if _, named := types.Unalias(object.Type()).(*types.Named); !named {
					indexMembers(index, object, types.Unalias(object.Type()))
				}
				continue
			}
			indexMembers(index, object, object.Type().Underlying())
		case *types.Var:
			indexMembers(index, object, object.Type())
		}
	}
	resolver.members[pkg] = index
	return index
}

func indexMembers(index map[types.Object]memberOwner, owner types.Object, typ types.Type) {
	switch typ := typ.(type) {
	case *types.Struct:
		for i := range typ.NumFields() {
			field := typ.Field(i)
			claimMember(index, field, memberOwner{owner: owner, tag: typ.Tag(i)})
			indexMembers(index, field, field.Type())
		}
	case *types.Interface:
		for i := range typ.NumExplicitMethods() {
			claimMember(index, typ.ExplicitMethod(i), memberOwner{owner: owner})
		}
	}
}

// claimMember keeps, for a literal shared by several declarations (type A B shares B's struct), the
// owner declared closest before the member, which is the one whose declaration contains it.
func claimMember(index map[types.Object]memberOwner, member types.Object, candidate memberOwner) {
	current, claimed := index[member]
	if !claimed || (candidate.owner.Pos() <= member.Pos() && (current.owner.Pos() > member.Pos() || candidate.owner.Pos() > current.owner.Pos())) {
		index[member] = candidate
	}
}

// parameterTypes encodes a signature's ordered parameter types; the variadic one is written ...T.
func parameterTypes(signature *types.Signature) ([]string, bool) {
	parameters := make([]string, 0, signature.Params().Len())
	for i := range signature.Params().Len() {
		parameter := signature.Params().At(i).Type()
		prefix := ""
		if signature.Variadic() && i == signature.Params().Len()-1 {
			if slice, ok := parameter.(*types.Slice); ok {
				prefix, parameter = "...", slice.Elem()
			}
		}
		encoded, valid := encodeType(parameter)
		if !valid {
			return nil, false
		}
		parameters = append(parameters, prefix+encoded)
	}
	return parameters, true
}
