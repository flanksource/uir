package schemagen

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// GenerateOpenAPI projects the canonical schema into an OpenAPI model catalog.
func GenerateOpenAPI(dir string) ([]byte, error) {
	encoded, err := Generate(dir)
	if err != nil {
		return nil, err
	}
	var schema Schema
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return nil, fmt.Errorf("decode generated schema: %w", err)
	}
	var definitions map[string]any
	definitionsJSON, err := json.Marshal(schema.Defs)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(definitionsJSON, &definitions); err != nil {
		return nil, err
	}
	if err := openAPIDiscriminators(schema, definitions); err != nil {
		return nil, err
	}
	openAPIRefinements(definitions)
	openAPIRefs(definitions)
	return json.MarshalIndent(map[string]any{
		"openapi":    "3.1.0",
		"info":       map[string]string{"title": "UIR models", "version": "1.0.0"},
		"paths":      map[string]any{},
		"components": map[string]any{"schemas": definitions},
	}, "", "  ")
}

func openAPIRefinements(definitions map[string]any) {
	for name, definition := range definitions {
		properties, ok := definition.(map[string]any)["properties"].(map[string]any)
		if !ok || properties["statement_refinement"] == nil {
			continue
		}
		refinement := properties["statement_refinement"].(map[string]any)
		component := strings.TrimPrefix(name, statementUnion) + "Refinement"
		definitions[component] = refinement
		properties["statement_refinement"] = map[string]any{"$ref": "#/components/schemas/" + component}
	}
}

func openAPIDiscriminators(schema Schema, definitions map[string]any) error {
	for name, key := range map[string]string{nodeUnion: nodeDiscriminator, statementUnion: statementDiscriminator} {
		mapping := map[string]string{}
		var kinds []string
		kindRef := "#/components/schemas/" + name + "Kind"
		for _, branch := range schema.Defs[name].OneOf {
			definitionName := strings.TrimPrefix(branch.Ref, "#/$defs/")
			definition := schema.Defs[definitionName]
			if definition == nil || definition.Properties[key] == nil {
				return fmt.Errorf("%s has a branch without %s: %s", name, key, branch.Ref)
			}
			kind := definition.Properties[key].Const
			kinds = append(kinds, kind)
			mapping[kind] = "#/components/schemas/" + definitionName
		}
		slices.Sort(kinds)
		definitions[name+"Kind"] = map[string]any{"type": "string", "enum": kinds}
		union := definitions[name].(map[string]any)
		union["discriminator"] = map[string]any{"propertyName": key, "mapping": mapping}
		union["properties"] = map[string]any{key: map[string]any{"$ref": kindRef}}
		for _, value := range definitions {
			if properties, ok := value.(map[string]any)["properties"].(map[string]any); ok {
				if property, ok := properties[key].(map[string]any); ok && property["const"] != nil {
					delete(property, "type")
					property["$ref"] = kindRef
				}
			}
		}
	}
	return nil
}

func openAPIRefs(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "$ref" {
				value[key] = strings.Replace(child.(string), "#/$defs/", "#/components/schemas/", 1)
			} else {
				openAPIRefs(child)
			}
		}
	case []any:
		for _, child := range value {
			openAPIRefs(child)
		}
	}
}
