package schemagen

import (
	"encoding/json"
	"fmt"
)

func GenerateJavaConfig(openAPI []byte) ([]byte, error) {
	var document struct {
		Components struct {
			Schemas map[string]Schema `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(openAPI, &document); err != nil {
		return nil, fmt.Errorf("decode OpenAPI catalog: %w", err)
	}
	if len(document.Components.Schemas) == 0 {
		return nil, fmt.Errorf("OpenAPI catalog has no schemas")
	}
	mappings := map[string]string{}
	for name, schema := range document.Components.Schemas {
		if schema.Type == "string" && (len(schema.AnyOf) > 0 || len(schema.OneOf) > 0) {
			mappings[name] = "java.lang.String"
		}
	}
	return json.MarshalIndent(map[string]any{"schemaMappings": mappings}, "", "  ")
}
