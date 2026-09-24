package uir

import (
	"maps"
	"slices"
	"time"
)

type NodeTreeMixin interface {
	Node
	GetChildren() []Node
}

type NodeTree struct {
	Node
}

type WalkOptions struct {
	// If true, will include the root node in the walk
	IncludeRoot bool
	// Maximum search depth below the root, 0 means unlimited
	Depth int
	// level is how far below the walk's root the current node is.
	level int
	// Node types to descend into, but not call f on
	Hide       []NodeType
	HideFilter NodeFilter

	// Skip node types entirely
	Skip       []NodeType
	SkipFilter NodeFilter

	// Node types to stop descending into, but still call f on
	Stop       []NodeType
	StopFilter NodeFilter
}

type NodeFilter func(Node) bool

func (options WalkOptions) WithSkipper(filter NodeFilter) WalkOptions {
	options.SkipFilter = filter
	return options
}

func (options WalkOptions) WithStopper(filter NodeFilter) WalkOptions {
	options.StopFilter = filter
	return options
}

func (options WalkOptions) WithHider(filter NodeFilter) WalkOptions {
	options.HideFilter = filter
	return options
}

func (opts WalkOptions) WithDepth(depth int) WalkOptions {
	opts.Depth = depth
	return opts
}

func mergeOptions(options []WalkOptions) WalkOptions {
	merged := WalkOptions{}
	for _, opt := range options {
		merged.IncludeRoot = merged.IncludeRoot || opt.IncludeRoot
		merged.Depth += opt.Depth
		merged.level = max(merged.level, opt.level)
		merged.Hide = append(merged.Hide, opt.Hide...)
		merged.Skip = append(merged.Skip, opt.Skip...)
		merged.Stop = append(merged.Stop, opt.Stop...)
		if opt.HideFilter != nil {
			merged.HideFilter = opt.HideFilter
		}
		if opt.SkipFilter != nil {
			merged.SkipFilter = opt.SkipFilter
		}
		if opt.StopFilter != nil {
			merged.StopFilter = opt.StopFilter
		}
	}
	return merged
}

func (tree NodeTree) GetLastModified() time.Time {
	var max time.Time
	_ = tree.Walk(func(node Node) bool {
		loc := node.GetLocation()
		if loc.LastModified != nil && loc.LastModified.After(max) {
			max = *loc.LastModified
		}
		return true
	})
	return max
}

// Walk calls f depth-first on every node below the root, and on the root when
// IncludeRoot is set. Hide suppresses f but still descends, Skip suppresses
// both, and Stop calls f without descending. f returning false ends the walk,
// and Walk then returns false.
func (tree NodeTree) Walk(f func(Node) bool, options ...WalkOptions) bool {
	option := mergeOptions(options)
	node := tree.Node
	nodeType := node.GetIdentifier().GetNodeType()

	if matchesNodeFilter(node, nodeType, option.Skip, option.SkipFilter) {
		return true
	}
	hidden := !option.IncludeRoot || matchesNodeFilter(node, nodeType, option.Hide, option.HideFilter)
	if !hidden && !f(node) {
		return false
	}
	if matchesNodeFilter(node, nodeType, option.Stop, option.StopFilter) || (option.Depth > 0 && option.level >= option.Depth) {
		return true
	}

	child := option
	child.IncludeRoot = true
	child.level++
	for _, c := range tree.GetChildren() {
		if !(NodeTree{Node: c}).Walk(f, child) {
			return false
		}
	}
	return true
}

// matchesNodeFilter applies filter when set, otherwise membership in types.
func matchesNodeFilter(node Node, nodeType NodeType, types []NodeType, filter NodeFilter) bool {
	if filter != nil {
		return filter(node)
	}
	return slices.Contains(types, nodeType)
}

// Returns a unique key for the relationship between two nodes by type
func GetRelationshipKey(rel Relationship) string {
	s := ""
	if rel.GetFrom() != nil {
		s += rel.GetFrom().GetIdentifier().String()
	}
	s += "->"
	if rel.GetTo() != nil {
		s += rel.GetTo().GetIdentifier().String()
	}
	s += "@"
	s += string(rel.GetRelationshipType())
	return s
}

// GetRelationships returns all unique relationships in the tree between nodes and their relationship type
func (tree NodeTree) GetRelationships() []Relationship {
	relationships := map[string]Relationship{}
	_ = tree.Walk(func(node Node) bool {
		if relatable, ok := node.(Relatable); ok {
			for _, rel := range relatable.GetRelationships() {
				relationships[GetRelationshipKey(rel)] = rel
			}
		}
		return true
	}, WalkOptions{})
	return slices.Collect(maps.Values(relationships))
}

func (tree NodeTree) GetLocations() []Location {

	locations := map[string]Location{}
	_ = tree.Walk(func(node Node) bool {
		loc := node.GetLocation()
		if loc.Path != "" {
			locations[loc.String()] = loc
		}
		return true
	}, WalkOptions{})
	return slices.Collect(maps.Values(locations))
}

func (tree NodeTree) GetFiles() []string {
	fileSet := make(map[string]struct{})
	_ = tree.Walk(func(node Node) bool {
		if node.GetLocation().Path != "" {
			fileSet[node.GetLocation().Path] = struct{}{}
		}
		return true
	}, WalkOptions{})
	return slices.Sorted(maps.Keys(fileSet))
}

// GroupByPackage groups the tree's top-level nodes by package, sorted by
// package name: same-named packages are merged, and nodes outside any package
// are grouped under the empty package. It descends only through containers
// (UIR, modules, node lists); a package or other node is grouped whole.
func (tree NodeTree) GroupByPackage() ([]PackageNode, error) {
	packages := map[string]*PackageNode{}
	var group func(node Node) error
	group = func(node Node) error {
		switch n := node.(type) {
		case UIR, *UIR, ModuleNode, *ModuleNode, NodeList, NodeTree:
			for _, child := range n.GetChildren() {
				if err := group(child); err != nil {
					return err
				}
			}
			return nil
		case *PackageNode:
			return group(*n)
		case PackageNode:
			if existing, ok := packages[n.Package]; ok {
				existing.Types = append(existing.Types, n.Types...)
				existing.Records = append(existing.Records, n.Records...)
				existing.Tables = append(existing.Tables, n.Tables...)
				existing.Endpoints = append(existing.Endpoints, n.Endpoints...)
				existing.Functions = append(existing.Functions, n.Functions...)
				existing.Variables = append(existing.Variables, n.Variables...)
				existing.InitFunctions = append(existing.InitFunctions, n.InitFunctions...)
				return nil
			}
			packages[n.Package] = &n
			return nil
		}
		unpackaged, ok := packages[""]
		if !ok {
			unpackaged = &PackageNode{}
			packages[""] = unpackaged
		}
		return unpackaged.Add(node)
	}
	if err := group(tree.Node); err != nil {
		return nil, err
	}

	grouped := make([]PackageNode, 0, len(packages))
	for _, name := range slices.Sorted(maps.Keys(packages)) {
		grouped = append(grouped, *packages[name])
	}
	return grouped, nil
}

func NewTree(nodes ...Node) NodeTree {
	return NodeTree{
		Node: NodeList(nodes),
	}
}
