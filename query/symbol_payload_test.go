package query_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("a declaration's payload", func() {
	DescribeTable("comes back on the query match of a published symbol that carries one and is omitted from one that does not",
		func(ctx SpecContext, backend string) {
			pipeline, scope := insurancePipeline(ctx, openQueryDatabase(ctx, backend))

			record := runQuery(ctx, pipeline, "kind:record", scope)
			Expect(record.Matches).To(HaveLen(1))
			Expect(string(record.Matches[0].Payload)).To(MatchJSON(policyPayload))
			encoded, err := json.Marshal(record.Matches[0])
			Expect(err).ToNot(HaveOccurred())
			var withPayload map[string]json.RawMessage
			Expect(json.Unmarshal(encoded, &withPayload)).To(Succeed())
			Expect(string(withPayload["payload"])).To(MatchJSON(policyPayload))

			table := runQuery(ctx, pipeline, "kind:table", scope)
			Expect(table.Matches).To(HaveLen(1))
			Expect(table.Matches[0].Payload).To(BeNil())
			encoded, err = json.Marshal(table.Matches[0])
			Expect(err).ToNot(HaveOccurred())
			var withoutPayload map[string]json.RawMessage
			Expect(json.Unmarshal(encoded, &withoutPayload)).To(Succeed())
			Expect(withoutPayload).ToNot(HaveKey("payload"))
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"),
	)

	DescribeTable("is the caller's own copy, so changing it leaves the pipeline's cached document as published",
		func(ctx SpecContext, backend string) {
			pipeline, scope := insurancePipeline(ctx, openQueryDatabase(ctx, backend))
			first := runQuery(ctx, pipeline, "kind:record", scope)
			Expect(first.Matches).To(HaveLen(1))
			for i := range first.Matches[0].Payload {
				first.Matches[0].Payload[i] = ' '
			}

			second := runQuery(ctx, pipeline, "kind:record", scope)
			Expect(second.Matches).To(HaveLen(1))
			Expect(string(second.Matches[0].Payload)).To(MatchJSON(policyPayload))
		},
		Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"),
	)
})
