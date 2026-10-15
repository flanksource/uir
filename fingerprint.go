package uir

import (
	"fmt"
	"sort"
	"strings"
)

// Layer names one of the three Merkle-composed hash layers of a node.
type Layer string

const (
	// LayerPublic is what a consumer outside the node's visibility boundary can
	// observe: an exported signature, a table's output columns.
	LayerPublic Layer = "public"
	// LayerPrivate is the full declared structure: the public facts plus
	// non-public members, constraints, defaults and declared order.
	LayerPrivate Layer = "private"
	// LayerContent is the implementation: lowered bodies and view definitions.
	LayerContent Layer = "content"
)

// Fingerprints are a node's three layer hashes. Private digests Public, so a
// public change is always a private change. Content is "" for a node with no
// implementation anywhere beneath it. Statistics (row counts), locations and
// source text are in no layer.
type Fingerprints struct {
	Public  string `json:"public"`
	Private string `json:"private"`
	Content string `json:"content"`
}

// Hash digests the three layers; it is what every registered node's Hash()
// returns, so the layers are the single source of truth.
func (f Fingerprints) Hash() string {
	h := NewHasher("fingerprints")
	h.AddString("public", f.Public)
	h.AddString("private", f.Private)
	h.AddString("content", f.Content)
	return h.String()
}

// Fingerprint computes node's three layer hashes. It panics on a node type it
// has no layers for, as WalkStatements does, so a newly registered node type
// cannot silently hash as nothing.
func Fingerprint(node Node) Fingerprints {
	return layersOf(node).fingerprints()
}

// Shape renders node's public or private layer as canonical text, one member
// per line, so a line diff shows what changed. The layer's hash digests this
// text, so a shape diff never disagrees with its hash. The content layer has no
// text and renders "".
func Shape(node Node, layer Layer) string {
	l := layersOf(node)
	switch layer {
	case LayerPublic:
		return l.public.text()
	case LayerPrivate:
		return l.private.text()
	case LayerContent:
		return ""
	}
	panic(fmt.Sprintf("uir.Shape: unknown layer %q", layer))
}

// shape is one layer of a node: its kind, its own facts inline, and one line per
// member. A node's own name is not part of its shape — it is the node's
// identity, and keeping it out lets a rename keep its hash.
type shape struct {
	kind    NodeType
	facts   string
	members []string
}

func (s shape) text() string {
	head := string(s.kind) + s.facts
	if len(s.members) == 0 {
		return head
	}
	var b strings.Builder
	b.WriteString(head + " {\n")
	for _, member := range s.members {
		b.WriteString("  " + member + "\n")
	}
	b.WriteString("}")
	return b.String()
}

type layers struct {
	public, private shape
	content         string
}

func (l layers) fingerprints() Fingerprints {
	public := NewHasher("fingerprint.public")
	public.AddString("shape", l.public.text())
	private := NewHasher("fingerprint.private")
	private.AddString("public", public.String())
	private.AddString("shape", l.private.text())
	return Fingerprints{Public: public.String(), Private: private.String(), Content: l.content}
}

// leafLayers is a node without members; privateFacts restate the public ones.
func leafLayers(kind NodeType, publicFacts, privateFacts, content string) layers {
	return layers{
		public:  shape{kind: kind, facts: publicFacts},
		private: shape{kind: kind, facts: privateFacts},
		content: content,
	}
}

func layersOf(node Node) layers {
	switch n := node.(type) {
	case ModuleNode:
		return moduleLayers(n)
	case *ModuleNode:
		return moduleLayers(*n)
	case PackageNode:
		return packageLayers(n)
	case *PackageNode:
		return packageLayers(*n)
	case TypedNode:
		return typedLayers(n)
	case *TypedNode:
		return typedLayers(*n)
	case MethodNode:
		return methodLayers(n)
	case *MethodNode:
		return methodLayers(*n)
	case ASTRecord:
		return recordLayers(n)
	case *ASTRecord:
		return recordLayers(*n)
	case ASTEndpoint:
		return endpointLayers(n)
	case *ASTEndpoint:
		return endpointLayers(*n)
	case RecordField:
		return fieldLayers(n)
	case *RecordField:
		return fieldLayers(*n)
	}
	return tableLayersOf(node)
}

func tableLayersOf(node Node) layers {
	switch n := node.(type) {
	case RecordTable:
		return tableLayers(n)
	case *RecordTable:
		return tableLayers(*n)
	case RecordColumn:
		return columnLayers(n)
	case *RecordColumn:
		return columnLayers(*n)
	case RecordIndex:
		return indexLayers(n)
	case *RecordIndex:
		return indexLayers(*n)
	case RecordForeignKey:
		return foreignKeyLayers(n)
	case *RecordForeignKey:
		return foreignKeyLayers(*n)
	case NodeRef:
		return refLayers(n)
	case *NodeRef:
		return refLayers(*n)
	}
	panic(fmt.Sprintf("uir.Fingerprint: no layers for node type %T", node))
}

// ShapeKind is the kind a node's shape opens with: its identifier's node type,
// falling back to its Go type's.
func ShapeKind(node Node) NodeType {
	if kind := node.GetIdentifier().GetNodeType(); kind != NodeTypeUnknown {
		return kind
	}
	return node.GetType()
}

// MemberName is how a container's shape names a member: its name, and its
// signature when it has one.
func MemberName(node Node) string {
	id := node.GetIdentifier()
	if id.Signature != "" {
		return id.GetName() + "#" + id.Signature
	}
	return id.GetName()
}

// MemberLine is exactly the line a container's Shape in layer embeds node as,
// without its indentation. It panics on the content layer, which has no text.
func MemberLine(node Node, layer Layer) string {
	return memberOf(node).line(layer)
}

// member is a child as its container composes it: by its name relative to the
// container, so moving or renaming the container does not change its hash.
type member struct {
	name   string
	public bool
	layers layers
	prints Fingerprints
}

func memberOf(node Node) member {
	l := layersOf(node)
	return member{name: MemberName(node), public: IsPublic(node), layers: l, prints: l.fingerprints()}
}

// line embeds the member in its container's shape: inline when it has no members
// of its own, else by its layer hash, which is what makes the layers Merkle.
func (m member) line(layer Layer) string {
	var s shape
	var hash string
	switch layer {
	case LayerPublic:
		s, hash = m.layers.public, m.prints.Public
	case LayerPrivate:
		s, hash = m.layers.private, m.prints.Private
	default:
		panic(fmt.Sprintf("uir.MemberLine: layer %q has no member lines", layer))
	}
	line := string(s.kind) + " " + m.name + s.facts
	if len(s.members) > 0 {
		line += " {…} " + hash
	}
	return line
}

// IsPublic reports whether node, as a member, reaches its container's public
// layer: it is public or has no visibility.
func IsPublic(node Node) bool {
	visibility := VisibilityNA
	switch n := node.(type) {
	case RecordField:
		visibility = n.Visibility
	case *RecordField:
		visibility = n.Visibility
	case MethodNode:
		visibility = n.Visibility
	case *MethodNode:
		visibility = n.Visibility
	case TypedNode:
		visibility = n.Visibility
	case *TypedNode:
		visibility = n.Visibility
	}
	return visibility == VisibilityPublic || visibility == VisibilityNA
}

// memberOrder is how a group of members is laid out in each layer.
type memberOrder int

const (
	// declaredOrder keeps the source order in both layers (fields, parameters).
	declaredOrder memberOrder = iota
	// unordered sorts both layers (methods, package members, indexes).
	unordered
	// orderedPrivately sorts the public layer and keeps declared order in the
	// private one: a table's columns are a set to a consumer, their order is
	// structure.
	orderedPrivately
)

// composition accumulates a container's member lines per layer.
type composition struct {
	public, private, content []string
	// hidden is set when a member was left out of the public layer for its
	// visibility, which struct-like containers mark as "opaque".
	hidden bool
}

// add composes nodes; privateOnly members never reach the public layer and do
// not make the container opaque (a table's indexes are not hidden fields).
func (c *composition) add(nodes []Node, order memberOrder, privateOnly bool) {
	var public, private []string
	for _, node := range nodes {
		m := memberOf(node)
		if !privateOnly && m.public {
			public = append(public, m.line(LayerPublic))
		} else if !privateOnly {
			c.hidden = true
		}
		private = append(private, m.line(LayerPrivate))
		if m.prints.Content != "" {
			c.content = append(c.content, string(m.layers.public.kind)+" "+m.name+" "+m.prints.Content)
		}
	}
	if order != declaredOrder {
		sort.Strings(public)
	}
	if order == unordered {
		sort.Strings(private)
	}
	c.public = append(c.public, public...)
	c.private = append(c.private, private...)
}

// opaque marks a struct-like container that has non-public members.
func (c *composition) opaque() {
	if c.hidden {
		c.public = append(c.public, "opaque")
	}
}

// layers closes the container; its own facts are public, so both layers carry them.
func (c *composition) layers(kind NodeType, facts, ownContent string) layers {
	return layers{
		public:  shape{kind: kind, facts: facts, members: c.public},
		private: shape{kind: kind, facts: facts, members: c.private},
		content: composeContent(ownContent, c.content),
	}
}

// composeContent digests a node's own implementation with its members' content,
// keyed by member and sorted. It is "" when there is neither.
func composeContent(own string, members []string) string {
	if own == "" && len(members) == 0 {
		return ""
	}
	sort.Strings(members)
	h := NewHasher("fingerprint.content")
	h.AddString("own", own)
	h.AddStringSlice("members", members)
	return h.String()
}
