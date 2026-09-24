package uir_test

import (
	"encoding/json"
	"math"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

// The persistent-body helpers write a node's heavy half to its own column; a body
// that cannot be encoded must fail the write, not store an empty body.
var _ = Describe("persistent bodies", func() {
	// encoding/json refuses NaN, and the codec refuses a node slot holding a typed nil.
	nan := math.NaN()
	unencodable := uir.TypedValue{Float: &nan}
	var missingRecord *uir.ASTRecord

	method := uir.NewMethod("compute").Build()
	method.Body = &uir.BlockStmt{Children: []uir.Statement{uir.NewRecordRead(uir.RecordTypeTable, missingRecord).Build()}}

	record := uir.NewRecord("AsPolicy").Build()
	record.Fields = []uir.RecordField{{DefaultValue: &unencodable}}

	endpoint := uir.NewEndpoint("getPolicy").Build()
	endpoint.Errors = []uir.ASTError{{Code: unencodable}}

	DescribeTable("report a body they cannot encode",
		func(persist func() (json.RawMessage, error)) {
			body, err := persist()
			Expect(err).To(HaveOccurred())
			Expect(body).To(BeNil())
		},
		Entry("MethodNode.Marshal", method.Marshal),
		Entry("PackageNode.GetPersistentBody", uir.NewPackage("com.example").WithFunction(method).Build().GetPersistentBody),
		Entry("ASTRecord.GetPersistentBody", record.GetPersistentBody),
		Entry("ASTEndpoint.GetPersistentBody", endpoint.GetPersistentBody),
	)

	It("round-trips a package's functions through its persistent body", func() {
		pkg := uir.NewPackage("com.example").WithFunction(uir.NewMethod("compute").Build()).Build()

		body, err := pkg.GetPersistentBody()
		Expect(err).NotTo(HaveOccurred())
		var loaded uir.PackageNode
		Expect(loaded.LoadPersistentBody(body)).To(Succeed())
		Expect(loaded.Functions).To(Equal(pkg.Functions))
	})
})
