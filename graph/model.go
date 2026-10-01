// Package graph builds a bounded, cycle-safe nodes-and-edges graph of calls from
// any Source of neighbours: the UIR relationships of a lowered body
// (FromRelationships) or an index. It knows nothing about a language; a Theme
// renames and restyles what a Source reports.
package graph

import (
	"context"

	"github.com/flanksource/uir"
)

const (
	DefaultDepth = 2
	DefaultLimit = 150
	MaxDepth     = 8
	MaxLimit     = 1000
)

// Graph is what Build returns. Nodes are ordered by depth then id, and Edges by
// from, to, then type; every edge's ends are in Nodes.
type Graph struct {
	Roots   []string `json:"roots"`
	Nodes   []Node   `json:"nodes"`
	Edges   []Edge   `json:"edges"`
	Groups  []Group  `json:"groups,omitempty"`
	Omitted Omitted  `json:"omitted"`
}

type Node struct {
	// ID is defined by the Source: a symbol id, or Identifier.IdentityKey().
	ID         string         `json:"id"`
	Identifier uir.Identifier `json:"identifier"`
	// Kind is a uir.NodeType unless a Theme renames it.
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Group string `json:"group,omitempty"`
	// Depth is 0 for a root, negative for its callers and positive for its callees.
	Depth int `json:"depth"`
	// In and Out count the node's incoming and outgoing edges, so that the
	// difference from the edges drawn is how many more there are to expand. Build
	// asks the Source for a node's neighbours only away from the root: Out is the
	// Source's total for a node at depth >= 0 when callees are walked, and In for
	// a node at depth <= 0 when callers are walked. In the direction Build did not
	// ask about, and for an unresolved node, which is never asked, the count is
	// the edges drawn, so it may be short of the Source's total. Edges a Theme
	// drops and edges to nodes Options.Exclude leaves out are not counted.
	In  int `json:"in"`
	Out int `json:"out"`
	// Unresolved marks a target the Source could not place; it is drawn as a leaf.
	Unresolved bool              `json:"unresolved,omitempty"`
	Location   *Location         `json:"location,omitempty"`
	Tone       string            `json:"tone,omitempty"`
	Icon       string            `json:"icon,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Location is where a node is declared, in the terms an index addresses it by.
type Location struct {
	RootKey      string `json:"root_key,omitempty"`
	CheckoutPath string `json:"checkout_path,omitempty"`
	SnapshotID   string `json:"snapshot_id,omitempty"`
	SourceID     string `json:"source_id,omitempty"`
	Path         string `json:"path,omitempty"`
	IdentityKey  string `json:"identity_key,omitempty"`
	Line         int    `json:"line,omitempty"`
	Column       int    `json:"column,omitempty"`
}

// Edge is every call from one node to another of one type.
type Edge struct {
	// ID is from|to|type.
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	// Type is uir.RelationshipTypeCall or uir.RelationshipTypeDispatch.
	Type uir.RelationshipType `json:"type"`
	Kind string               `json:"kind,omitempty"`
	// Sites are the call sites behind the edge, ordered by path, line and column.
	// The edge is conditional when every site has guards.
	Sites []Site `json:"sites"`
}

type Site struct {
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	// Text is the call as written.
	Text string `json:"text,omitempty"`
	// Guards are the conditions that must hold to reach the site, outermost
	// first, as plain text.
	Guards []string `json:"guards,omitempty"`
}

type Group struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Parent string `json:"parent,omitempty"`
}

// Omitted is what the graph leaves out.
type Omitted struct {
	// NodeLimit reports that nodes were left out because of Options.Limit.
	NodeLimit bool `json:"node_limit,omitempty"`
	// BeyondDepth counts the nodes at Options.Depth that have neighbours further out.
	BeyondDepth int `json:"beyond_depth,omitempty"`
	// Unresolved counts the call sites on edges to unresolved nodes; an edge
	// without sites counts as one.
	Unresolved int `json:"unresolved,omitempty"`
	// UnreadableSource lists the files whose guards a Source could not derive.
	UnreadableSource []string `json:"unreadable_source,omitempty"`
	// Excluded counts, per Node.Group, the distinct nodes Options.Exclude left
	// out. A node only a node at Options.Depth reaches is not counted.
	Excluded map[string]int `json:"excluded,omitempty"`
}

// Step is a neighbour and the edge to it. A Source sets the edge's Type, Kind
// and Sites; Build sets its ID, From and To from the direction it walked.
type Step struct {
	Node Node
	Edge Edge
}

// Source reports a node and its neighbours. Out are the nodes id calls and In
// the nodes that call id; several steps to one neighbour with one edge type are
// merged into one edge.
type Source interface {
	Describe(ctx context.Context, id string) (Node, error)
	Out(ctx context.Context, id string) ([]Step, error)
	In(ctx context.Context, id string) ([]Step, error)
}

// Theme restyles a graph. Node is applied to every node once its Depth, In and
// Out are final and must leave its ID as it is. Edge is applied to every edge,
// and returning false drops it: the edge is not drawn, counted or followed.
type Theme interface {
	Node(*Node)
	Edge(*Edge) bool
}

type Direction string

const (
	DirectionCallees Direction = "callees"
	DirectionCallers Direction = "callers"
	DirectionBoth    Direction = "both"
)

// Options bound a Build. Nothing is defaulted: Direction must be set, Depth must
// be 1..MaxDepth and Limit 1..MaxLimit. A nil Theme leaves the graph as the
// Source reported it.
type Options struct {
	Direction Direction
	// Depth is how many edges away from a root the graph reaches.
	Depth int
	// Limit is the most nodes the graph holds.
	Limit int
	Theme Theme
	// Exclude reports a neighbour to leave out: it is not added, drawn, counted
	// in In or Out, or walked, and Omitted.Excluded tallies it. A root and a
	// node already in the graph are never excluded. A nil Exclude keeps every
	// node.
	Exclude func(Node) bool
}
