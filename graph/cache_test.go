package graph_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
)

// memoryCache is a graph.Cache in memory that counts its builds.
type memoryCache struct {
	stored map[string][]byte
	builds int
}

func (c *memoryCache) Load(_ context.Context, params []byte, decode func([]byte) error, build func() ([]byte, error)) (bool, error) {
	key := graph.ParamsKey(params)
	if raw, hit := c.stored[key]; hit {
		return true, decode(raw)
	}
	raw, err := build()
	if err != nil {
		return false, err
	}
	c.stored[key] = raw
	c.builds++
	return false, decode(raw)
}

var _ = Describe("Params", func() {
	params := func() graph.Params {
		return graph.Params{
			Codec: "cg1", Roots: []string{"transaction:0A"}, Direction: graph.DirectionCallees, Depth: 2, Limit: 150,
			ExcludeKinds: []string{"Table", "copybook"}, ExcludeGroups: []string{"Plan · B", "Global"},
			Access: []uir.RelationshipType{uir.RelationshipTypeCall}, Columns: true, Guards: true,
			Inputs: map[string]string{"index": "g7", "schema": "d1"},
		}
	}
	encode := func(p graph.Params) string {
		GinkgoHelper()
		raw, err := p.Encode()
		Expect(err).NotTo(HaveOccurred())
		return string(raw)
	}

	It("encodes a request canonically: both exclusion lists lowercased and sorted, inputs by name", func() {
		Expect(encode(params())).To(Equal(`{"codec":"cg1","roots":["transaction:0A"],"direction":"callees","depth":2,"limit":150,` +
			`"excludeKinds":["copybook","table"],"excludeGroups":["global","plan · b"],"access":["call"],"columns":true,"guards":true,` +
			`"inputs":{"index":"g7","schema":"d1"}}`))
	})

	It("encodes differently whatever the request changes", func() {
		changed := params()
		changed.Inputs = map[string]string{"index": "g8", "schema": "d1"}
		reordered := params()
		reordered.ExcludeKinds = []string{"COPYBOOK", "table"}
		reordered.ExcludeGroups = []string{"GLOBAL", "plan · b"}

		Expect([]bool{encode(changed) == encode(params()), encode(reordered) == encode(params())}).To(Equal([]bool{false, true}))
	})

	It("keys params by the hex SHA-256 of their encoding", func() {
		sum := sha256.Sum256([]byte(`{"root":"a"}`))
		Expect(graph.ParamsKey([]byte(`{"root":"a"}`))).To(Equal(hex.EncodeToString(sum[:])))
	})
})

var _ = Describe("Cached", func() {
	type built struct {
		Graph    graph.Graph `json:"graph"`
		Warnings []string    `json:"warnings"`
	}
	params := []byte(`{"root":"a"}`)
	builder := func(warning string, builds *int) func() (built, error) {
		return func() (built, error) {
			*builds++
			return built{Graph: graph.Graph{Roots: []string{"a"}}, Warnings: []string{warning}}, nil
		}
	}

	It("builds once and serves the stored graph after, decoding what it stored", func() {
		cache, builds := &memoryCache{stored: map[string][]byte{}}, 0
		first, firstHit, err := graph.Cached(context.Background(), cache, params, builder("first", &builds))
		Expect(err).NotTo(HaveOccurred())
		second, secondHit, err := graph.Cached(context.Background(), cache, params, builder("second", &builds))
		Expect(err).NotTo(HaveOccurred())

		Expect([]any{first, firstHit, second, secondHit, builds}).To(Equal([]any{
			built{Graph: graph.Graph{Roots: []string{"a"}}, Warnings: []string{"first"}}, false,
			built{Graph: graph.Graph{Roots: []string{"a"}}, Warnings: []string{"first"}}, true, 1,
		}))
	})

	It("builds every time without a cache", func() {
		builds := 0
		for range 2 {
			_, hit, err := graph.Cached(context.Background(), nil, params, builder("w", &builds))
			Expect([]any{hit, err}).To(Equal([]any{false, nil}))
		}
		Expect(builds).To(Equal(2))
	})

	It("fails when the build does", func() {
		broken := errors.New("index missing")
		_, _, err := graph.Cached(context.Background(), &memoryCache{stored: map[string][]byte{}}, params, func() (built, error) { return built{}, broken })
		Expect(err).To(MatchError(broken))
	})
})
