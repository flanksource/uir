// Command genschema regenerates schema/uir.schema.json from the Go model and
// python/statement_kinds.py from the statement registry. Run it with
// `make schema`; TestSchemaIsUpToDate and TestPythonStatementKindsIsUpToDate fail
// when it has not been.
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
	flag.Parse()

	schema, err := schemagen.Generate(*src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generating schema: %v\n", err)
		os.Exit(1)
	}
	write(*out, schema)
	write(*pythonOut, schemagen.GeneratePythonStatementKinds())
}

func write(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "writing %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes)\n", path, len(data))
}
