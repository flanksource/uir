package uir_test

import (
	"encoding/json"

	"github.com/flanksource/uir"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SQL migration metadata", func() {
	It("preserves catalogue facts through the UIR JSON codec", func() {
		input := []byte(`{"tables":[{"package":"dbo","type":"orders","sourceCode":{},"checks":[{"name":"CK_total","expression":"total >= 0"}],"columns":[{"field":"id","sourceCode":{},"sqlType":"int","identity":{"seed":1000,"increment":5},"sqlDefault":{"name":"DF_id","expression":"(0)"},"collation":"Latin1_General_CI_AS"}],"indexes":[{"field":"UX_orders","sourceCode":{},"columns":["id"],"descending":["id"],"constraint":true}]}]}`)
		var document uir.UIR
		Expect(uir.Unmarshal(input, &document)).To(Succeed())
		encoded, err := json.Marshal(document)
		Expect(err).NotTo(HaveOccurred())
		var actual, expected map[string]any
		Expect(json.Unmarshal(encoded, &actual)).To(Succeed())
		Expect(json.Unmarshal(input, &expected)).To(Succeed())
		Expect(actual).To(Equal(expected))
	})

	DescribeTable("fingerprints migration facts",
		func(before, after string) {
			var left, right uir.RecordTable
			Expect(uir.Unmarshal([]byte(before), &left)).To(Succeed())
			Expect(uir.Unmarshal([]byte(after), &right)).To(Succeed())
			Expect(left.Hash()).NotTo(Equal(right.Hash()))
		},
		Entry("identity seed", `{"columns":[{"field":"id","sqlType":"int","identity":{"seed":1,"increment":1}}]}`, `{"columns":[{"field":"id","sqlType":"int","identity":{"seed":10,"increment":1}}]}`),
		Entry("default constraint", `{"columns":[{"field":"id","sqlDefault":{"name":"DF_old","expression":"(0)"}}]}`, `{"columns":[{"field":"id","sqlDefault":{"name":"DF_new","expression":"(0)"}}]}`),
		Entry("index direction", `{"indexes":[{"field":"IX_id","columns":["id"]}]}`, `{"indexes":[{"field":"IX_id","columns":["id"],"descending":["id"]}]}`),
		Entry("check expression", `{"checks":[{"name":"CK_total","expression":"total > 0"}]}`, `{"checks":[{"name":"CK_total","expression":"total >= 0"}]}`),
		Entry("trigger disabled state", `{"triggers":[{"method":"audit","properties":{"isDisabled":false}}]}`, `{"triggers":[{"method":"audit","properties":{"isDisabled":true}}]}`),
		Entry("resolved module dependencies", `{"sqlDependencies":[{"schema":"dbo","name":"orders"}]}`, `{"sqlDependencies":[{"schema":"audit","name":"orders"}]}`),
	)
})
