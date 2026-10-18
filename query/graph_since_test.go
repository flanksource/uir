package query_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	chainModule = "example.org/chain"
	chainSource = `package chain

type Notifier interface {
	Notify()
}

type Mail struct{}

func (Mail) Notify() {}

func A(notifier Notifier) {
	B()
	notifier.Notify()
}

func B() { C() }

func C() {}

func Unused() {}
`
)

// changedNode is a changed node of a change query, named by labels.
type changedNode struct {
	Label  string
	Reason query.GraphChangeReason
	Path   []string
}

func changedNodes(result query.GraphResult) []changedNode {
	labels := map[string]string{}
	for _, node := range result.Nodes {
		labels[node.ID] = node.Label
	}
	changed := []changedNode{}
	for _, node := range result.Since.Nodes {
		path := make([]string, 0, len(node.Path))
		for _, id := range node.Path {
			path = append(path, labels[id])
		}
		changed = append(changed, changedNode{Label: labels[node.ID], Reason: node.Reason, Path: path})
	}
	return changed
}

var _ = Describe("the call graph changes since a snapshot", func() {
	DescribeTable("reports every changed node A reaches with the path to it",
		func(ctx SpecContext, from, to string, expected []changedNode) {
			database := openQueryDatabase(ctx, "sqlite")
			checkout := filepath.Join(GinkgoT().TempDir(), "chain")
			Expect(os.MkdirAll(checkout, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module "+chainModule+"\n\ngo 1.26\n"), 0o644)).To(Succeed())
			write := func(source string) {
				Expect(os.WriteFile(filepath.Join(checkout, "chain.go"), []byte(source), 0o644)).To(Succeed())
			}
			write(from)
			before := indexCheckout(ctx, database, checkout)
			write(to)
			indexCheckout(ctx, database, checkout)
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())

			result := callGraph(ctx, pipeline, query.GraphOptions{
				Selector: chainModule + ".A", Depth: 3, Since: &query.GraphSince{Revision: "before", SnapshotID: uuid.MustParse(before.SnapshotID)},
				Scope: query.ModuleScopeOptions{RootKey: chainModule, Location: checkout},
			})

			Expect(result.Since).ToNot(BeNil())
			Expect(result.Since.Revision).To(Equal("before"))
			Expect(result.Since.Changed).To(Equal(len(expected) > 0))
			Expect(changedNodes(result)).To(Equal(expected))
			Expect(result.Nodes[0].Depth).To(Equal(0), "a change query walks callees only")
			for _, node := range result.Nodes {
				Expect(node.Depth).To(BeNumerically(">=", 0))
			}
		},
		Entry("a body change two calls deep", chainSource, replace(chainSource, "func C() {}", "func C() { _ = 1 }"),
			[]changedNode{{Label: "C", Reason: query.GraphChangeBody, Path: []string{"A", "B", "C"}}}),
		Entry("a signature change of a callee", chainSource, replace(chainSource, "func B() { C() }", "func B() error { C(); return nil }"),
			[]changedNode{{Label: "B", Reason: query.GraphChangeSignature, Path: []string{"A", "B"}}}),
		Entry("a body change of an implementation reached by interface dispatch", chainSource, replace(chainSource, "func (Mail) Notify() {}", "func (Mail) Notify() { _ = 1 }"),
			[]changedNode{{Label: "Mail.Notify", Reason: query.GraphChangeBody, Path: []string{"A", "Mail.Notify"}}}),
		Entry("a change outside the graph", chainSource, replace(chainSource, "func Unused() {}", "func Unused() { _ = 1 }"), []changedNode{}),
		Entry("an unchanged graph", chainSource, chainSource, []changedNode{}),
	)

	It("refuses a change query that walks callers", func(ctx SpecContext) {
		pipeline, err := query.NewPipeline(openQueryDatabase(ctx, "sqlite"))
		Expect(err).ToNot(HaveOccurred())
		_, err = pipeline.Graph(ctx, query.GraphOptions{Selector: chainModule + ".A", Direction: graph.DirectionCallers, Since: &query.GraphSince{Revision: "before", SnapshotID: uuid.New()}})
		Expect(err).To(MatchError(ContainSubstring("a change query walks callees")))
	})
})

func replace(source, old, replacement string) string {
	if strings.Count(source, old) != 1 {
		panic(fmt.Sprintf("%q is not in the fixture exactly once", old))
	}
	return strings.Replace(source, old, replacement, 1)
}
