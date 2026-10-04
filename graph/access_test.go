package graph_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

var _ = Describe("Access", func() {
	var (
		call     = uir.RelationshipTypeCall
		dispatch = uir.RelationshipTypeDispatch
		read     = uir.RelationshipTypeRead
		write    = uir.RelationshipTypeWrite
	)

	DescribeTable("ParseAccess returns each type once, in call, read, write order",
		func(values []string, want []uir.RelationshipType) {
			Expect(graph.ParseAccess(values)).To(Equal(want))
		},
		Entry("nil, every type, when none is named", nil, []uir.RelationshipType(nil)),
		Entry("nil, every type, when only blanks are named", []string{" ", ","}, []uir.RelationshipType(nil)),
		Entry("comma separated, in any case and order", []string{"Write, read", "call"}, []uir.RelationshipType{call, read, write}),
		Entry("a repeated type once", []string{"read", "read,READ"}, []uir.RelationshipType{read}),
	)

	It("refuses an access type that is not call, read or write", func() {
		_, err := graph.ParseAccess([]string{"read,delete"})
		Expect(err).To(MatchError(`graph: access "delete" is not one of call, read, write`))
	})

	DescribeTable("Follows",
		func(access []uir.RelationshipType, edge uir.RelationshipType, want bool) {
			Expect(graph.Follows(access, edge)).To(Equal(want))
		},
		Entry("no access follows a write", nil, write, true),
		Entry("no access follows an import", nil, uir.RelationshipTypeImport, true),
		Entry("call follows a dispatch", []uir.RelationshipType{call}, dispatch, true),
		Entry("call does not follow a read", []uir.RelationshipType{call}, read, false),
		Entry("read follows a read", []uir.RelationshipType{read, write}, read, true),
		Entry("read does not follow a call", []uir.RelationshipType{read}, call, false),
	)

	Describe("Options.Access", func() {
		src := calls("root>helper").
			with(stubEdge{from: "root", to: "dispatched", kind: dispatch}).
			with(stubEdge{from: "root", to: "table", kind: read}).
			with(stubEdge{from: "helper", to: "table", kind: write}).
			with(stubEdge{from: "audit", to: "table", kind: read})
		accessGraph := func(direction graph.Direction, root string, access ...uir.RelationshipType) shape {
			GinkgoHelper()
			return shapeOf(buildWith(src, graph.Options{Direction: direction, Depth: 2, Limit: graph.DefaultLimit, Access: access}, root))
		}

		It("follows only the listed edge types, counting only the edges it follows", func() {
			Expect(accessGraph(graph.DirectionCallees, "root", read, write)).To(Equal(shape{
				Nodes: []string{"root@0 in=0 out=1", "table@1 in=1 out=0"},
				Edges: []string{"root|table|read"},
			}))
		})

		It("follows a dispatch under call", func() {
			Expect(accessGraph(graph.DirectionCallees, "root", call)).To(Equal(shape{
				Nodes: []string{"root@0 in=0 out=2", "dispatched@1 in=1 out=0", "helper@1 in=1 out=0"},
				Edges: []string{"root|dispatched|dispatch", "root|helper|call"},
			}))
		})

		It("follows every edge type when none is listed", func() {
			Expect(accessGraph(graph.DirectionCallers, "table").Edges).To(Equal([]string{
				"audit|table|read", "helper|table|write", "root|helper|call", "root|table|read",
			}))
		})

		DescribeTable("refuses an access entry that is not a relationship type",
			func(entry uir.RelationshipType) {
				_, err := graph.Build(context.Background(), src, []string{"root"},
					graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: 1, Access: []uir.RelationshipType{read, entry}})
				Expect(err).To(MatchError(fmt.Sprintf("graph: access lists %q, which is not a relationship type", entry)))
			},
			Entry("no edge type", uir.RelationshipType("")),
			Entry("a misspelt one", uir.RelationshipType("reads")),
		)

		It("accepts every relationship type in access", func() {
			all := []uir.RelationshipType{
				uir.RelationshipTypeImport, call, uir.RelationshipTypeReference, uir.RelationshipTypeInheritance, uir.RelationshipTypeImplements,
				uir.RelationshipTypeIncludes, uir.RelationshipTypeForeignKey, read, write, dispatch,
			}
			_, err := graph.Build(context.Background(), src, []string{"root"}, graph.Options{Direction: graph.DirectionCallees, Depth: 1, Limit: 1, Access: all})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
