// Command genschema regenerates the JSON Schema and OpenAPI model catalog from
// Go, and Python statement kinds from the registry. Run it with `task schema`;
// the schema tests fail when generated artifacts no longer match the model.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/flanksource/uir/internal/schemagen"
)

func main() {
	src := flag.String("src", ".", "directory holding the uir package source")
	out := flag.String("out", "schema/uir.schema.json", "file to write the schema to")
	pythonOut := flag.String("python-out", "python/statement_kinds.py", "file to write the Python statement kinds to")
	openAPIOut := flag.String("openapi-out", "schema/uir.openapi.json", "file to write the OpenAPI model catalog to")
	javaOut := flag.String("java-config-out", "schema/java-generator.json", "file to write Java schema mappings to")
	check := flag.Bool("check", false, "report stale artifacts without writing files")
	flag.Parse()

	schema, err := schemagen.Generate(*src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generating schema: %v\n", err)
		os.Exit(1)
	}
	openAPI, err := schemagen.GenerateOpenAPI(*src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generating OpenAPI: %v\n", err)
		os.Exit(1)
	}
	javaConfig, err := schemagen.GenerateJavaConfig(openAPI)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generating Java configuration: %v\n", err)
		os.Exit(1)
	}
	outputs := []artifact{
		{Path: *out, Data: schema},
		{Path: *openAPIOut, Data: append(openAPI, '\n')},
		{Path: *javaOut, Data: append(javaConfig, '\n')},
		{Path: *pythonOut, Data: schemagen.GeneratePythonStatementKinds()},
	}
	if err := updateArtifacts(outputs, artifactOptions{Check: *check}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, output := range outputs {
		if *check {
			fmt.Printf("current %s\n", output.Path)
		} else {
			fmt.Printf("wrote %s (%d bytes)\n", output.Path, len(output.Data))
		}
	}
}
