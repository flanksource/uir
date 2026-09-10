package uir

import (
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/flanksource/commons/collections"
	"github.com/samber/lo"
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
	// Maximum search depth, 0 means unlimited

	Depth int
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

type MatchExpression string

func (me MatchExpression) Matches(value string) (matches bool, negated bool) {
	patterns := strings.Split(string(me), ",")
	return collections.MatchItem(value, patterns...)
}

func (opts WalkOptions) WithDepth(depth int) WalkOptions {
	opts.Depth = depth
	return opts
}

func (opts WalkOptions) HideRoot(hide bool) WalkOptions {
	opts.IncludeRoot = hide
	return opts
}

func mergeOptions(options []WalkOptions) WalkOptions {
	merged := WalkOptions{}
	for _, opt := range options {
		merged.IncludeRoot = merged.IncludeRoot || opt.IncludeRoot
		merged.Depth += opt.Depth
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

func (tree NodeTree) Walk(f func(Node) bool, options ...WalkOptions) bool {
	option := mergeOptions(options)
	shouldHide := !option.IncludeRoot
	shouldSkip := false
	shouldStop := false

	node := tree.Node
	nodeType := node.GetIdentifier().GetNodeType()

	if !shouldHide && option.HideFilter != nil {
		shouldHide = option.HideFilter(tree)
	} else if !shouldHide {
		for _, nt := range option.Hide {
			if nodeType == nt {
				shouldHide = true
				break
			}
		}
	}

	if option.SkipFilter != nil {
		shouldSkip = option.SkipFilter(tree)
	} else if !shouldSkip {
		for _, nt := range option.Skip {
			if nodeType == nt {
				shouldSkip = true
				break
			}
		}
	}

	if option.StopFilter != nil {
		shouldStop = option.StopFilter(tree)
	} else if !shouldStop {
		for _, nt := range option.Stop {
			if nodeType == nt {
				shouldStop = true
				break
			}
		}
	}

	if shouldStop {
		return false
	}
	if shouldSkip {
		return true
	}

	if !shouldHide {
		if !f(tree) {
			return false
		}
	}
	for _, child := range tree.GetChildren() {
		tree := NodeTree{Node: child}
		if !tree.Walk(f, option.HideRoot(shouldHide)) {
			return false
		}
	}
	return true
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
	return lo.Values(relationships)
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
	return lo.Values(locations)
}

func (tree NodeTree) GetFiles() []string {
	fileSet := make(map[string]struct{})
	_ = tree.Walk(func(node Node) bool {
		if node.GetLocation().Path != "" {
			fileSet[node.GetLocation().Path] = struct{}{}
		}
		return true
	}, WalkOptions{})
	files := lo.Keys(fileSet)
	sort.Strings(files)
	return files
}

// GroupByPackage groups nodes by their package, nodes without a package are grouped under an empty package
func (tree NodeTree) GroupByPackage() ([]PackageNode, error) {
	packages := map[string]PackageNode{}
	walkErr := atomic.Value{}
	_ = tree.Walk(func(node Node) bool {
		pkgName := ""
		var pkg PackageNode
		if pkgNode, ok := node.(PackageNode); ok {
			pkgName = pkgNode.Package
			if existing, ok := packages[pkgName]; !ok {
				pkg = node.(PackageNode)
			} else {
				pkg = existing
			}
		}
		if err := pkg.Add(node); err != nil {
			walkErr.Store(err)
			return false
		}
		packages[pkgName] = pkg
		return true
	}, WalkOptions{})
	if err := walkErr.Load(); err != nil {
		return nil, err.(error)
	}
	return lo.Values(packages), nil
}

func NewTree(nodes ...Node) NodeTree {
	return NodeTree{
		Node: NodeList(nodes),
	}
}
