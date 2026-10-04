package schemagen

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/flanksource/uir"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestContract(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Schema wire contract")
}

var _ = Describe("Schema wire contract", func() {
	var schema *Schema
	BeforeEach(func() {
		encoded, err := Generate(sourceDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(encoded, &schema)).To(Succeed())
	})

	It("requires non-omitempty fields without making concrete node tags mandatory", func() {
		encoded, err := json.Marshal(schema.Defs["RecordField"])
		Expect(err).NotTo(HaveOccurred())
		var object map[string]any
		Expect(json.Unmarshal(encoded, &object)).To(Succeed())
		Expect(object["required"]).To(ContainElement("fieldType"))
		Expect(object["required"]).NotTo(ContainElement(nodeDiscriminator))
	})

	DescribeTable("requires a unique discriminator in polymorphic slots",
		func(name, key string) {
			union := schema.Defs[name]
			Expect(union.AnyOf).To(BeEmpty())
			Expect(union.OneOf).NotTo(BeEmpty())
			seen := map[string]bool{}
			for _, branch := range union.OneOf {
				definition := deref(branch, schema.Defs)
				Expect(definition).NotTo(BeNil())
				encoded, err := json.Marshal(definition)
				Expect(err).NotTo(HaveOccurred())
				var object map[string]any
				Expect(json.Unmarshal(encoded, &object)).To(Succeed())
				Expect(object["required"]).To(ContainElement(key))
				kind := definition.Properties[key].Const
				Expect(kind).NotTo(BeEmpty())
				Expect(seen[kind]).To(BeFalse(), "duplicate discriminator %s", kind)
				seen[kind] = true
			}
		},
		Entry("nodes", nodeUnion, nodeDiscriminator),
		Entry("statements", statementUnion, statementDiscriminator),
	)

	It("validates every registered node and statement with a draft 2020-12 validator", func() {
		encoded, err := Generate(sourceDir)
		Expect(err).NotTo(HaveOccurred())
		resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		Expect(err).NotTo(HaveOccurred())
		compiler := jsonschema.NewCompiler()
		Expect(compiler.AddResource("uir.json", resource)).To(Succeed())
		for _, registry := range []struct {
			name   string
			values []json.RawMessage
		}{
			{name: nodeUnion, values: encodedNodes()},
			{name: statementUnion, values: encodedStatements()},
		} {
			validator, err := compiler.Compile("uir.json#/$defs/" + registry.name)
			Expect(err).NotTo(HaveOccurred())
			for _, encoded := range registry.values {
				value, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
				Expect(err).NotTo(HaveOccurred())
				Expect(validator.Validate(value)).To(Succeed(), "%s: %s", registry.name, encoded)
			}
			Expect(validator.Validate(map[string]any{})).NotTo(Succeed())
		}
	})

	It("accepts exactly the statement refinements that Go's registry decodes", func() {
		encoded, err := Generate(sourceDir)
		Expect(err).NotTo(HaveOccurred())
		resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		Expect(err).NotTo(HaveOccurred())
		compiler := jsonschema.NewCompiler()
		Expect(compiler.AddResource("uir.json", resource)).To(Succeed())
		validator, err := compiler.Compile("uir.json#/$defs/Statement")
		Expect(err).NotTo(HaveOccurred())
		for _, encoded := range encodedStatements() {
			var object map[string]any
			Expect(json.Unmarshal(encoded, &object)).To(Succeed())
			kind := object[statementDiscriminator].(string)
			refinements := []string{kind, kind + ":example", kind + ":", kind + ":\n", kind + "\n", "unknown", ""}
			for _, other := range uir.StatementMarshaler.Kinds() {
				refinements = append(refinements, other, other+":example")
			}
			for _, accepted := range uir.StatementMarshaler.CrossHierarchy() {
				refinements = append(refinements, accepted...)
			}
			for _, refined := range refinements {
				object[uir.StatementMarshaler.RefinementField()] = refined
				data, err := json.Marshal(object)
				Expect(err).NotTo(HaveOccurred())
				_, decodeErr := uir.StatementMarshaler.UnmarshalByType(data)
				Expect(validator.Validate(object) == nil).To(Equal(decodeErr == nil), "%s refined to %q", kind, refined)
			}
		}
	})
})

func encodedNodes() []json.RawMessage {
	var values []json.RawMessage
	for _, node := range uir.Nodes {
		encoded, err := uir.MarshalNode(node)
		Expect(err).NotTo(HaveOccurred())
		values = append(values, encoded)
	}
	return values
}

func encodedStatements() []json.RawMessage {
	var values []json.RawMessage
	for _, statement := range uir.Statements {
		encoded, err := uir.MarshalStatement(statement)
		Expect(err).NotTo(HaveOccurred())
		values = append(values, encoded)
	}
	return values
}
