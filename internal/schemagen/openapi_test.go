package schemagen

import (
	"bytes"
	"encoding/json"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var _ = Describe("OpenAPI projection", func() {
	It("keeps the schema choices and adds explicit discriminator mappings for Java", func() {
		encoded, err := GenerateOpenAPI(sourceDir)
		Expect(err).NotTo(HaveOccurred())
		var document struct {
			OpenAPI    string `json:"openapi"`
			Components struct {
				Schemas map[string]map[string]any `json:"schemas"`
			} `json:"components"`
		}
		Expect(json.Unmarshal(encoded, &document)).To(Succeed())
		Expect(document.OpenAPI).To(Equal("3.1.0"))
		for _, name := range []string{nodeUnion, statementUnion} {
			union := document.Components.Schemas[name]
			discriminator := union["discriminator"].(map[string]any)
			key := discriminator["propertyName"].(string)
			mapping := discriminator["mapping"].(map[string]any)
			Expect(mapping).To(HaveLen(len(union["oneOf"].([]any))))
			Expect(union).To(HaveKey("properties"))
			properties := union["properties"].(map[string]any)
			kind := properties[key].(map[string]any)
			Expect(kind["$ref"]).To(Equal("#/components/schemas/" + name + "Kind"))
			sharedKind := document.Components.Schemas[name+"Kind"]
			Expect(sharedKind["enum"]).To(HaveLen(len(mapping)))
			for value, target := range mapping {
				variant := document.Components.Schemas[target.(string)[len("#/components/schemas/"):]]
				property := variant["properties"].(map[string]any)[key].(map[string]any)
				Expect(property["$ref"]).To(Equal(kind["$ref"]))
				Expect(property["const"]).To(Equal(value))
			}
		}
		Expect(document.Components.Schemas[statementUnion]["discriminator"].(map[string]any)["mapping"]).To(HaveKey("dispatch_call"))
		Expect(string(encoded)).NotTo(ContainSubstring("#/$defs/"))
		Expect(document.Components.Schemas["RecordField"]["required"]).To(ContainElement("fieldType"))
		second, err := GenerateOpenAPI(sourceDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(second).To(Equal(encoded))
		committed, err := os.ReadFile("../../schema/uir.openapi.json")
		Expect(err).NotTo(HaveOccurred())
		Expect(committed).To(Equal(append(encoded, '\n')), "run make schema to regenerate the OpenAPI catalog")
	})

	It("preserves wire validation when discriminator enums are shared", func() {
		encoded, err := GenerateOpenAPI(sourceDir)
		Expect(err).NotTo(HaveOccurred())
		resource, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		Expect(err).NotTo(HaveOccurred())
		compiler := jsonschema.NewCompiler()
		Expect(compiler.AddResource("uir.openapi.json", resource)).To(Succeed())
		for _, registry := range []struct {
			name, key string
			values    []json.RawMessage
		}{
			{name: nodeUnion, key: nodeDiscriminator, values: encodedNodes()},
			{name: statementUnion, key: statementDiscriminator, values: encodedStatements()},
		} {
			validator, err := compiler.Compile("uir.openapi.json#/components/schemas/" + registry.name)
			Expect(err).NotTo(HaveOccurred())
			for _, data := range registry.values {
				var value map[string]any
				Expect(json.Unmarshal(data, &value)).To(Succeed())
				Expect(validator.Validate(value)).To(Succeed(), "%s", data)
				if registry.name == statementUnion {
					value["statement_refinement"] = value[registry.key].(string) + ":example"
					Expect(validator.Validate(value)).To(Succeed(), "%s", data)
					value["statement_refinement"] = "unknown"
					Expect(validator.Validate(value)).NotTo(Succeed())
					delete(value, "statement_refinement")
				}
				value[registry.key] = "unknown"
				Expect(validator.Validate(value)).NotTo(Succeed())
				delete(value, registry.key)
				Expect(validator.Validate(value)).NotTo(Succeed())
			}
		}
	})
})
