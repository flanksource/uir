package symbolhandle

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Kind is the 6-bit code of a symbols.kind value. Code 0 is reserved and never packed. The builtin kinds
// are fixed here and numbered upward from 1; a database allocates the codes of its custom kinds from
// MaxKind downward, so the two never meet unless the field is full.
type Kind uint8

const (
	KindPackage Kind = iota + 1
	KindType
	KindFunc
	KindMethod
	KindField
	KindVar
	KindConst
	KindBuiltin
	KindModule
	KindInterface
	KindConstructor
	KindRecord
	KindEndpoint
	KindTable
	KindColumn
	KindIndex
	KindForeignKey
	KindAnnotation
)

const (
	// MaxBuiltinKind is the highest builtin code; custom codes lie above it.
	MaxBuiltinKind = KindAnnotation
	// MaxKind is the largest code the kind field holds, the first a database allocates to a custom kind.
	MaxKind Kind = 1<<KindBits - 1
)

// Category is what a kind means to the query layer: a container groups symbols (package, module), a
// type declares a shape (type, interface, record, table), a callable is called (func, method,
// constructor, endpoint), and a member belongs to an owner or a scope (field, var, const, builtin,
// column, index, foreign_key, annotation). The callable category decides which symbols `<` and `>`
// read call occurrences of; the rest only pick selectors and node types.
type Category string

const (
	CategoryContainer Category = "container"
	CategoryType      Category = "type"
	CategoryCallable  Category = "callable"
	CategoryMember    Category = "member"
)

var categories = []Category{CategoryContainer, CategoryType, CategoryCallable, CategoryMember}

func ParseCategory(name string) (Category, error) {
	if !slices.Contains(categories, Category(name)) {
		return "", fmt.Errorf("symbol kind category %q is not one of %v", name, categories)
	}
	return Category(name), nil
}

// KindSpec is one kind of a registry: its code, its name as symbols.kind stores it, and its category.
type KindSpec struct {
	Code     Kind
	Name     string
	Category Category
}

var builtinKinds = [...]KindSpec{
	{KindPackage, "package", CategoryContainer}, {KindType, "type", CategoryType},
	{KindFunc, "func", CategoryCallable}, {KindMethod, "method", CategoryCallable},
	{KindField, "field", CategoryMember}, {KindVar, "var", CategoryMember},
	{KindConst, "const", CategoryMember}, {KindBuiltin, "builtin", CategoryMember},
	{KindModule, "module", CategoryContainer}, {KindInterface, "interface", CategoryType},
	{KindConstructor, "constructor", CategoryCallable}, {KindRecord, "record", CategoryType},
	{KindEndpoint, "endpoint", CategoryCallable}, {KindTable, "table", CategoryType},
	{KindColumn, "column", CategoryMember}, {KindIndex, "index", CategoryMember},
	{KindForeignKey, "foreign_key", CategoryMember}, {KindAnnotation, "annotation", CategoryMember},
}

// BuiltinKinds is every builtin kind, ordered by code.
func BuiltinKinds() []KindSpec { return slices.Clone(builtinKinds[:]) }

func (kind Kind) String() string {
	if kind >= KindPackage && kind <= MaxBuiltinKind {
		return builtinKinds[kind-1].Name
	}
	return fmt.Sprintf("Kind(%d)", uint8(kind))
}

// customKindName is a namespaced custom kind: lowercase dot-separated segments, at least two, such as
// oipa.rule. Builtin names have no dot.
var customKindName = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z][a-z0-9_-]*)+$`)

// ValidateCustomKind checks that name is a namespaced custom kind name and category is a category.
func ValidateCustomKind(name string, category Category) error {
	if !customKindName.MatchString(name) {
		return fmt.Errorf("custom symbol kind %q must be namespaced as <namespace>.<name> in lowercase", name)
	}
	_, err := ParseCategory(string(category))
	return err
}

// KindNamespace is the namespace of a custom kind name, the part before its first dot, and empty for a
// builtin.
func KindNamespace(name string) string {
	namespace, _, found := strings.Cut(name, ".")
	if !found {
		return ""
	}
	return namespace
}

// Kinds is a database's kind registry: the builtin kinds and its custom kinds, by name and by code.
type Kinds struct {
	byName map[string]KindSpec
	byCode map[Kind]KindSpec
}

// Builtins is the registry of the builtin kinds alone.
func Builtins() Kinds {
	kinds, err := NewKinds(nil)
	if err != nil {
		panic(err)
	}
	return kinds
}

// NewKinds is the registry of the builtin kinds and custom; a custom kind outside the custom codes,
// not namespaced, of an unknown category, or sharing a code or name is an error.
func NewKinds(custom []KindSpec) (Kinds, error) {
	kinds := Kinds{byName: make(map[string]KindSpec, len(builtinKinds)+len(custom)), byCode: make(map[Kind]KindSpec, len(builtinKinds)+len(custom))}
	for _, spec := range builtinKinds {
		kinds.byName[spec.Name], kinds.byCode[spec.Code] = spec, spec
	}
	for _, spec := range custom {
		if spec.Code <= MaxBuiltinKind || spec.Code > MaxKind {
			return Kinds{}, fmt.Errorf("symbol kind %q: code %d is not a custom code (%d through %d)", spec.Name, spec.Code, MaxBuiltinKind+1, MaxKind)
		}
		if err := ValidateCustomKind(spec.Name, spec.Category); err != nil {
			return Kinds{}, err
		}
		if existing, taken := kinds.byCode[spec.Code]; taken {
			return Kinds{}, fmt.Errorf("symbol kinds %q and %q share code %d", existing.Name, spec.Name, spec.Code)
		}
		if existing, taken := kinds.byName[spec.Name]; taken {
			return Kinds{}, fmt.Errorf("symbol kind %q has codes %d and %d", spec.Name, existing.Code, spec.Code)
		}
		kinds.byName[spec.Name], kinds.byCode[spec.Code] = spec, spec
	}
	return kinds, nil
}

// Lookup is the registered kind named name.
func (kinds Kinds) Lookup(name string) (KindSpec, error) {
	spec, found := kinds.byName[name]
	if !found {
		return KindSpec{}, fmt.Errorf("symbol kind %q is not registered", name)
	}
	return spec, nil
}

// Spec is the registered kind with code.
func (kinds Kinds) Spec(code Kind) (KindSpec, error) {
	spec, found := kinds.byCode[code]
	if !found {
		return KindSpec{}, fmt.Errorf("symbol kind code %d is not registered", code)
	}
	return spec, nil
}

// All is every registered kind, ordered by code.
func (kinds Kinds) All() []KindSpec {
	all := make([]KindSpec, 0, len(kinds.byCode))
	for _, spec := range kinds.byCode {
		all = append(all, spec)
	}
	slices.SortFunc(all, func(left, right KindSpec) int { return int(left.Code) - int(right.Code) })
	return all
}

// InCategory is the names of the registered kinds of category, ordered by code.
func (kinds Kinds) InCategory(category Category) []string {
	var names []string
	for _, spec := range kinds.All() {
		if spec.Category == category {
			names = append(names, spec.Name)
		}
	}
	return names
}

// Visibility is the 1-bit visibility code; exported sorts after internal.
type Visibility uint8

const (
	Internal Visibility = 0
	Exported Visibility = 1
)

func ParseVisibility(name string) (Visibility, error) {
	switch name {
	case "internal":
		return Internal, nil
	case "exported":
		return Exported, nil
	}
	return 0, fmt.Errorf("symbol visibility %q is neither exported nor internal", name)
}

func (visibility Visibility) String() string {
	switch visibility {
	case Internal:
		return "internal"
	case Exported:
		return "exported"
	}
	return fmt.Sprintf("Visibility(%d)", uint8(visibility))
}
