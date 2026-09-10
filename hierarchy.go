package uir

type HierarchyGraph struct {
	Nodes []HierarchyNode `json:"nodes,omitempty" gorm:"serializer:json"`
}

func mergeHierarchyGraphs(left, right *HierarchyGraph) *HierarchyGraph {
	switch {
	case left == nil || len(left.Nodes) == 0:
		if right == nil || len(right.Nodes) == 0 {
			return nil
		}
		out := *right
		out.Nodes = append([]HierarchyNode(nil), right.Nodes...)
		return &out
	case right == nil || len(right.Nodes) == 0:
		out := *left
		out.Nodes = append([]HierarchyNode(nil), left.Nodes...)
		return &out
	default:
		out := &HierarchyGraph{
			Nodes: append(append([]HierarchyNode(nil), left.Nodes...), right.Nodes...),
		}
		return out
	}
}

type HierarchyNode struct {
	ID          string                `json:"id,omitempty"`
	Kind        string                `json:"kind,omitempty"`
	Name        string                `json:"name,omitempty"`
	DisplayName string                `json:"displayName,omitempty"`
	Properties  map[string]string     `json:"properties,omitempty"`
	Values      map[string]TypedValue `json:"values,omitempty"`
	SourceCode  SourceCode            `json:"sourceCode,omitempty"`
	Children    []HierarchyEdge       `json:"children,omitempty" gorm:"serializer:json"`
	Attachments []HierarchyAttachment `json:"attachments,omitempty" gorm:"serializer:json"`
	Imports     []ImportSpec          `json:"imports,omitempty" gorm:"serializer:json"`
	Exports     []ExportSpec          `json:"exports,omitempty" gorm:"serializer:json"`
}

type HierarchyEdge struct {
	Name     string `json:"name,omitempty"`
	TargetID string `json:"targetId,omitempty"`
	EdgeKind string `json:"edgeKind,omitempty"`
	Order    int    `json:"order,omitempty"`
}

type HierarchyAttachment struct {
	Role          string       `json:"role,omitempty"`
	SymbolRef     UIRSymbolRef `json:"symbolRef,omitempty" gorm:"serializer:json"`
	PlacementName string       `json:"placementName,omitempty"`
}

type UIRSymbolRef struct {
	SymbolKind string `json:"symbolKind,omitempty"`
	Module     string `json:"module,omitempty"`
	Package    string `json:"package,omitempty"`
	Type       string `json:"type,omitempty"`
	Method     string `json:"method,omitempty"`
	Name       string `json:"name,omitempty"`
}

func SymbolRefForNode(node Node) UIRSymbolRef {
	id := node.GetIdentifier()
	ref := UIRSymbolRef{
		SymbolKind: string(node.GetType()),
		Module:     id.Module,
		Package:    id.Package,
		Type:       id.Type,
		Method:     id.Method,
		Name:       id.Field,
	}

	switch n := node.(type) {
	case TypedNode:
		ref.Name = n.Type
	case *TypedNode:
		ref.Name = n.Type
	case MethodNode:
		ref.Name = n.Method
	case *MethodNode:
		ref.Name = n.Method
	case PackageNode:
		ref.Name = n.Package
	case *PackageNode:
		ref.Name = n.Package
	case ModuleNode:
		ref.Name = n.Module
	case *ModuleNode:
		ref.Name = n.Module
	case ASTRecord:
		ref.Name = n.Type
	case *ASTRecord:
		ref.Name = n.Type
	case ASTEndpoint:
		ref.Name = n.Type
	case *ASTEndpoint:
		ref.Name = n.Type
	case RecordField:
		ref.Name = n.Field
	case *RecordField:
		ref.Name = n.Field
	case RecordTable:
		ref.Name = n.Type
	case *RecordTable:
		ref.Name = n.Type
	case RecordColumn:
		ref.Name = n.Field
	case *RecordColumn:
		ref.Name = n.Field
	case RecordIndex:
		ref.Name = n.Field
	case *RecordIndex:
		ref.Name = n.Field
	case RecordForeignKey:
		ref.Name = n.Field
	case *RecordForeignKey:
		ref.Name = n.Field
	}

	if ref.Name == "" {
		switch {
		case id.Method != "":
			ref.Name = id.Method
		case id.Type != "":
			ref.Name = id.Type
		case id.Package != "":
			ref.Name = id.Package
		case id.Module != "":
			ref.Name = id.Module
		}
	}

	return ref
}

type ImportSpec struct {
	Module        string            `json:"module,omitempty"`
	DefaultName   string            `json:"defaultName,omitempty"`
	NamespaceName string            `json:"namespaceName,omitempty"`
	Named         []ImportNamedSpec `json:"named,omitempty" gorm:"serializer:json"`
	TypeOnly      bool              `json:"typeOnly,omitempty"`
}

type ImportNamedSpec struct {
	Name  string `json:"name,omitempty"`
	Alias string `json:"alias,omitempty"`
}

type ExportSpec struct {
	Kind   string `json:"kind,omitempty"`
	Name   string `json:"name,omitempty"`
	Alias  string `json:"alias,omitempty"`
	Module string `json:"module,omitempty"`
}
