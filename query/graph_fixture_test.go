package query_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	flowModule = "example.org/flow"
	flowPath   = "flow.go"
	flowSource = `package flow

import (
	"errors"

	"example.org/flow/notify"
)

type Order struct {
	Total int
	Kind  string
	Ready bool
}

func Process(order Order, channel notify.Channel, retries int) error {
	if order.Total > 0 {
		charge(order)
	} else {
		refund(order)
	}
	if order.Kind == "gift" {
		wrap(order)
	} else if order.Ready {
		ship(order)
	}
	switch order.Kind {
	case "digital":
		deliver(order)
	case "gift", "bulk":
		pack(order)
	default:
		shelve(order)
	}
	switch {
	case order.Total > 100:
		audit(order)
	}
	for retries > 0 {
		retry(order)
		retries--
	}
	if valid(order) {
		record(order)
	}
	if order.Total < 0 {
		return errors.New("negative total")
	}
	return channel.Send(order.Kind)
}

func charge(Order)  {}
func refund(Order)  {}
func wrap(Order)    {}
func ship(Order)    {}
func deliver(Order) {}
func pack(Order)    {}
func shelve(Order)  {}
func audit(Order)   {}
func retry(Order)   {}
func record(Order)  {}

func valid(order Order) bool { return order.Total >= 0 }

func Refill(order Order) {
	charge(order)
	charge(order)
}

func Handle(order Order, channel notify.Channel) error { return Process(order, channel, 1) }

func Entry(channel notify.Channel) error { return Handle(Order{}, channel) }

func Top(channel notify.Channel) error { return Entry(channel) }

func countdown(n int) {
	if n > 0 {
		countdown(n - 1)
	}
}

func ping(n int) {
	if n > 0 {
		pong(n - 1)
	}
}

func pong(n int) { ping(n) }

func Apply(hook func()) {
	if hook != nil {
		hook()
	}
}
`
	flowNotify = `package notify

type Channel interface {
	Send(message string) error
}

type Email struct{}

func (Email) Send(message string) error { return nil }

type SMS struct{}

func (SMS) Send(message string) error { return nil }
`
)

// flowWorkspace writes the call graph fixture module: a function whose calls sit
// under each kind of guard, an interface with two implementations, a chain of
// callers, direct and mutual recursion, and a call through a function value.
func flowWorkspace() string {
	GinkgoHelper()
	checkout := filepath.Join(GinkgoT().TempDir(), "flow")
	Expect(os.MkdirAll(filepath.Join(checkout, "notify"), 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+flowModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, flowPath), []byte(flowSource), 0o644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(checkout, "notify", "notify.go"), []byte(flowNotify), 0o644)).To(Succeed())
	return checkout
}

func flowPipeline(ctx context.Context, database *gorm.DB) (*query.Pipeline, query.ModuleScopeOptions, string) {
	GinkgoHelper()
	checkout := flowWorkspace()
	indexCheckout(ctx, database, checkout)
	pipeline, err := query.NewPipeline(database)
	Expect(err).ToNot(HaveOccurred())
	return pipeline, query.ModuleScopeOptions{RootKey: flowModule, Location: checkout}, checkout
}

func callGraph(ctx context.Context, pipeline *query.Pipeline, options query.GraphOptions) query.GraphResult {
	GinkgoHelper()
	result, err := pipeline.Graph(ctx, options)
	Expect(err).ToNot(HaveOccurred(), "graph of %s%s", options.Selector, options.Symbol)
	return result
}

// flowLine is the line of the fixture that holds snippet, found in the source text.
func flowLine(snippet string) int {
	GinkgoHelper()
	Expect(strings.Count(flowSource, snippet)).To(Equal(1), "%q appears once in the fixture", snippet)
	before, _, _ := strings.Cut(flowSource, snippet)
	return strings.Count(before, "\n") + 1
}

func graphNode(result query.GraphResult, label string) graph.Node {
	GinkgoHelper()
	position := slices.IndexFunc(result.Nodes, func(node graph.Node) bool { return node.Label == label })
	Expect(position).To(BeNumerically(">=", 0), "the graph has a node labelled %s, it has %v", label, graphLabels(result))
	return result.Nodes[position]
}

func graphLabels(result query.GraphResult) []string {
	labels := make([]string, 0, len(result.Nodes))
	for _, node := range result.Nodes {
		labels = append(labels, node.Label)
	}
	slices.Sort(labels)
	return labels
}

type graphEdge struct {
	From, To string
	Type     uir.RelationshipType
}

// graphEdges names each edge by the labels of its ends.
func graphEdges(result query.GraphResult) []graphEdge {
	labels := map[string]string{}
	for _, node := range result.Nodes {
		labels[node.ID] = node.Label
	}
	edges := make([]graphEdge, 0, len(result.Edges))
	for _, edge := range result.Edges {
		edges = append(edges, graphEdge{From: labels[edge.From], To: labels[edge.To], Type: edge.Type})
	}
	return edges
}

func edgeBetween(result query.GraphResult, from, to string, kind uir.RelationshipType) graph.Edge {
	GinkgoHelper()
	id := graphNode(result, from).ID + "|" + graphNode(result, to).ID + "|" + string(kind)
	position := slices.IndexFunc(result.Edges, func(edge graph.Edge) bool { return edge.ID == id })
	Expect(position).To(BeNumerically(">=", 0), "the graph has a %s edge from %s to %s, it has %v", kind, from, to, graphEdges(result))
	return result.Edges[position]
}
