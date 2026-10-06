package schemagen

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Java generator configuration", func() {
	It("maps constrained string unions to Java strings without changing enum models", func() {
		openAPI, err := GenerateOpenAPI(sourceDir)
		Expect(err).NotTo(HaveOccurred())
		encoded, err := GenerateJavaConfig(openAPI)
		Expect(err).NotTo(HaveOccurred())
		var config struct {
			SchemaMappings map[string]string `json:"schemaMappings"`
		}
		Expect(json.Unmarshal(encoded, &config)).To(Succeed())
		Expect(config.SchemaMappings).To(HaveKeyWithValue("MethodCallStmtRefinement", "java.lang.String"))
		Expect(config.SchemaMappings).To(HaveKeyWithValue("BlockStmtRefinement", "java.lang.String"))
		Expect(config.SchemaMappings).NotTo(HaveKey("NodeKind"))
		Expect(config.SchemaMappings).NotTo(HaveKey("StatementKind"))
		Expect(config.SchemaMappings).NotTo(HaveKey("Node"))
		Expect(config.SchemaMappings).NotTo(HaveKey("Statement"))
		second, err := GenerateJavaConfig(openAPI)
		Expect(err).NotTo(HaveOccurred())
		Expect(second).To(Equal(encoded))
	})

	It("rejects an invalid OpenAPI catalog", func() {
		_, err := GenerateJavaConfig([]byte(`{"components":`))
		Expect(err).To(HaveOccurred())
	})
})
