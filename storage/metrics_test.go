package storage_test

import (
	"encoding/json"

	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Callable metric records", func() {
	DescribeTable("rejects incomplete or invalid metric records instead of treating missing counts as zero", func(input string) {
		var metrics storage.CallableMetrics
		err := json.Unmarshal([]byte(input), &metrics)
		if err == nil {
			err = metrics.Validate(symbolhandle.CategoryCallable)
		}
		Expect(err).To(HaveOccurred())
	},
		Entry("missing parameter count", `{"version":1,"body_lines":1}`),
		Entry("missing body availability", `{"version":1,"param_count":0}`),
		Entry("negative parameter count", `{"version":1,"param_count":-1,"body_lines":1}`),
		Entry("zero body span", `{"version":1,"param_count":0,"body_lines":0}`),
		Entry("unknown semantics", `{"version":2,"param_count":0,"body_lines":null}`),
		Entry("unknown metric", `{"version":1,"param_count":0,"body_lines":null,"typo":1}`),
	)

	It("round trips known zero parameters and a known bodyless declaration", func() {
		expected := storage.CallableMetrics{Version: storage.MetricsVersion, ParamCount: 0}
		encoded, err := json.Marshal(expected)
		Expect(err).NotTo(HaveOccurred())
		var actual storage.CallableMetrics
		Expect(json.Unmarshal(encoded, &actual)).To(Succeed())
		Expect(actual).To(Equal(expected))
		Expect(actual.Validate(symbolhandle.CategoryCallable)).To(Succeed())
		Expect(actual.Validate(symbolhandle.CategoryType)).To(HaveOccurred())
	})
})
