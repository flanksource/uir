// Command genschema regenerates schema/uir.schema.json from the Go model.
// Run it with `make schema`; TestSchemaIsUpToDate fails when it has not been.
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
	flag.Parse()

	schema, err := schemagen.Generate(*src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generating schema: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, schema, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "writing %s: %v\n", *out, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(schema))
}
