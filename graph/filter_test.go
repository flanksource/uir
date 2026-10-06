package graph_test

import (
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

var _ = Describe("Filter", func() {
	It("names the flags every frontend of clicky-ui's CallGraph sends alike", func() {
		flags := []string{}
		filter := reflect.TypeFor[graph.Filter]()
		for i := range filter.NumField() {
			flags = append(flags, filter.Field(i).Tag.Get("flag"))
		}
		Expect(flags).To(Equal([]string{"direction", "depth", "limit", "access", "columns"}))
	})

	It("asks Build for the options it names, taking the default depth and limit for zero", func() {
		opts, err := graph.Filter{Direction: "callers", Depth: 3, Limit: 40, Access: []string{"write,read"}}.Options()
		Expect(err).NotTo(HaveOccurred())

		Expect([]any{opts.Direction, opts.Depth, opts.Limit, opts.Access, opts.Exclude == nil}).To(Equal([]any{
			graph.DirectionCallers, 3, 40, []uir.RelationshipType{uir.RelationshipTypeRead, uir.RelationshipTypeWrite}, true,
		}))
	})

	It("follows every edge type, as empty Options do, when it names no access", func() {
		opts, err := graph.Filter{Direction: "both"}.Options()
		Expect(err).NotTo(HaveOccurred())

		Expect(opts).To(Equal(graph.Options{Direction: graph.DirectionBoth, Depth: graph.DefaultDepth, Limit: graph.DefaultLimit}))
	})

	It("refuses an access type it does not know", func() {
		_, err := graph.Filter{Direction: "callees", Access: []string{"delete"}}.Options()
		Expect(err).To(MatchError(`graph: access "delete" is not one of call, read, write`))
	})
})
