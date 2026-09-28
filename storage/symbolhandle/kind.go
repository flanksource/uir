package symbolhandle

import "fmt"

// Kind is the 3-bit code of a symbols.kind value; there are exactly eight kinds.
type Kind uint8

const (
	KindPackage Kind = iota
	KindType
	KindFunc
	KindMethod
	KindField
	KindVar
	KindConst
	KindBuiltin
)

var kindNames = [...]string{"package", "type", "func", "method", "field", "var", "const", "builtin"}

func ParseKind(name string) (Kind, error) {
	for code, known := range kindNames {
		if known == name {
			return Kind(code), nil
		}
	}
	return 0, fmt.Errorf("symbol kind %q has no handle code", name)
}

func (kind Kind) String() string {
	if int(kind) < len(kindNames) {
		return kindNames[kind]
	}
	return fmt.Sprintf("Kind(%d)", uint8(kind))
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
