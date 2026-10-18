package indexer

import (
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// implementsFact is one implements fact by canonical ids: a type the root declares and an interface it
// satisfies by value or by pointer.
type implementsFact struct {
	typeID, interfaceID string
}

// snapshotImplements relates every non-interface, non-generic defined type the root's typed packages
// declare to the candidate interfaces (candidateInterfaces) it satisfies. It runs over the whole load
// after documents are extracted, so a type is related to interfaces outside its own import closure,
// and a reused package's types are related to interfaces another package added. Every candidate is a
// function of what the snapshot's context hash covers, so equal contexts give equal facts.
func snapshotImplements(load *typedLoad, resolver *symbolResolver, shapes *exportShapes, states []packageState) ([]implementsFact, error) {
	index := newInterfaceIndex(candidateInterfaces(load, shapes, states))
	found := map[implementsFact]bool{}
	for _, state := range states {
		for _, path := range sortedKeys(state.typed) {
			typed := state.typed[path]
			for _, declared := range fileDeclarations(typed.file, typed.pkg.TypesInfo) {
				named := implementer(declared.object)
				if named == nil {
					continue
				}
				resolved, err := resolver.resolve(declared.object)
				if err != nil {
					return nil, err
				}
				if resolved.ID == "" || !validDeclaration(declared.object) {
					continue
				}
				for _, candidate := range index.satisfiedBy(named) {
					implemented, err := resolver.resolve(candidate)
					if err != nil {
						return nil, err
					}
					if implemented.ID != "" {
						found[implementsFact{typeID: resolved.ID, interfaceID: implemented.ID}] = true
					}
				}
			}
		}
	}
	return sortedFacts(found), nil
}

// implementer is the defined type a declaration names when it can implement an interface: not an
// alias, an interface, or a generic type.
func implementer(object types.Object) *types.Named {
	typeName, ok := object.(*types.TypeName)
	if !ok || typeName.IsAlias() {
		return nil
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok || named.TypeParams().Len() > 0 || types.IsInterface(named) {
		return nil
	}
	return named
}

func sortedFacts(found map[implementsFact]bool) []implementsFact {
	facts := make([]implementsFact, 0, len(found))
	for fact := range found {
		facts = append(facts, fact)
	}
	sort.Slice(facts, func(i, j int) bool {
		left, right := facts[i], facts[j]
		return left.typeID < right.typeID || (left.typeID == right.typeID && left.interfaceID < right.interfaceID)
	})
	return facts
}

// interfaceAdmission is which interfaces of a package are candidates.
type interfaceAdmission int

const (
	admitAll interfaceAdmission = iota
	admitExported
	admitExportedMethods
)

// candidateInterfaces are the non-empty, non-generic, method-set interfaces declared at package level
// in the packages the root's typed variants reach through their imports, except that the walk does not
// pass through a workspace package of another module. Each is admitted by what keys it:
//   - the root's own packages, any interface: their sources are the snapshot's content set;
//   - standard-library and module-cache packages, exported interfaces: the toolchain and the module
//     versions key them;
//   - another module's workspace package whose export shape a package input hash folds in, exported
//     interfaces with only exported methods: that shape is all the snapshot's context covers of it, and
//     a type outside the package satisfies an unexported method only through what the shape omits.
func candidateInterfaces(load *typedLoad, shapes *exportShapes, states []packageState) []*types.TypeName {
	var candidates []*types.TypeName
	visited := map[string]bool{}
	var visit func(*packages.Package)
	visit = func(pkg *packages.Package) {
		if visited[pkg.ID] {
			return
		}
		visited[pkg.ID] = true
		origin := originOf(pkg)
		switch {
		case origin.Class == workspacePackage && origin.ModulePath == load.root.RootKey:
			candidates = append(candidates, packageInterfaces(pkg.Types, admitAll)...)
		case origin.Class == standardPackage || origin.Class == modulePackage:
			candidates = append(candidates, packageInterfaces(pkg.Types, admitExported)...)
		case origin.Class == workspacePackage && shapes.shaped(pkg):
			candidates = append(candidates, packageInterfaces(pkg.Types, admitExportedMethods)...)
			return
		default:
			return
		}
		for _, path := range sortedKeys(pkg.Imports) {
			visit(pkg.Imports[path])
		}
	}
	for _, state := range states {
		for _, variant := range state.variants {
			visit(variant)
		}
	}
	return candidates
}

func packageInterfaces(pkg *types.Package, admission interfaceAdmission) []*types.TypeName {
	var found []*types.TypeName
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		typeName, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || typeName.IsAlias() || (admission != admitAll && !typeName.Exported()) {
			continue
		}
		named, ok := typeName.Type().(*types.Named)
		if !ok || named.TypeParams().Len() > 0 {
			continue
		}
		iface, ok := named.Underlying().(*types.Interface)
		if !ok || !iface.IsMethodSet() || iface.NumMethods() == 0 || (admission == admitExportedMethods && !exportedMethods(iface)) {
			continue
		}
		found = append(found, typeName)
	}
	return found
}

func exportedMethods(iface *types.Interface) bool {
	for i := range iface.NumMethods() {
		if !iface.Method(i).Exported() {
			return false
		}
	}
	return true
}

// interfaceIndex finds the candidates a type satisfies without testing every one: a candidate is
// tested only when the type's method set has a method of each of its names.
type interfaceIndex struct {
	candidates []*types.TypeName
	byMethod   map[string][]int
	methods    *typeutil.MethodSetCache
}

func newInterfaceIndex(candidates []*types.TypeName) *interfaceIndex {
	index := &interfaceIndex{candidates: candidates, byMethod: map[string][]int{}, methods: &typeutil.MethodSetCache{}}
	for position, candidate := range candidates {
		iface := candidate.Type().Underlying().(*types.Interface)
		for i := range iface.NumMethods() {
			name := iface.Method(i).Name()
			index.byMethod[name] = append(index.byMethod[name], position)
		}
	}
	return index
}

// satisfiedBy is every candidate the type or a pointer to it implements, in candidate order.
func (index *interfaceIndex) satisfiedBy(named *types.Named) []*types.TypeName {
	pointer := types.NewPointer(named)
	set := index.methods.MethodSet(pointer)
	hits := map[int]int{}
	for i := range set.Len() {
		for _, position := range index.byMethod[set.At(i).Obj().Name()] {
			hits[position]++
		}
	}
	positions := make([]int, 0, len(hits))
	for position, count := range hits {
		iface := index.candidates[position].Type().Underlying().(*types.Interface)
		if count == iface.NumMethods() && (types.Implements(named, iface) || types.Implements(pointer, iface)) {
			positions = append(positions, position)
		}
	}
	sort.Ints(positions)
	satisfied := make([]*types.TypeName, len(positions))
	for i, position := range positions {
		satisfied[i] = index.candidates[position]
	}
	return satisfied
}
