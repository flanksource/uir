package schemagen

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/flanksource/uir"
)

const pythonStatementKindsPath = "../../python/statement_kinds.py"

// TestPythonStatementKindsIsUpToDate fails when a statement kind or cross-hierarchy
// refinement was registered without regenerating the module the Python encoder
// checks refinements against.
func TestPythonStatementKindsIsUpToDate(t *testing.T) {
	committed, err := os.ReadFile(pythonStatementKindsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", pythonStatementKindsPath, err)
	}
	if !bytes.Equal(GeneratePythonStatementKinds(), committed) {
		t.Fatalf("%s no longer matches uir's statement registry; run `make schema`", pythonStatementKindsPath)
	}
}

// TestPythonStatementKindsListsTheRegistry checks the module names every
// registered kind and every cross-hierarchy refinement, so the up-to-date check
// cannot pass on a generator that drops them.
func TestPythonStatementKindsListsTheRegistry(t *testing.T) {
	generated := string(GeneratePythonStatementKinds())
	for _, kind := range uir.StatementMarshaler.Kinds() {
		if !strings.Contains(generated, `    "`+kind+`",`+"\n") {
			t.Errorf("registered kind %q is missing from the generated module", kind)
		}
	}
	lines := strings.Split(generated, "\n")
	for kind, refinements := range uir.StatementMarshaler.CrossHierarchy() {
		prefix := `    "` + kind + `": frozenset([`
		i := slices.IndexFunc(lines, func(line string) bool { return strings.HasPrefix(line, prefix) })
		if i < 0 {
			t.Errorf("cross-hierarchy refinements of %q are missing from the generated module", kind)
			continue
		}
		for _, refinement := range refinements {
			if !strings.Contains(lines[i], `"`+refinement+`"`) {
				t.Errorf("cross-hierarchy refinement %q of %q is missing from %q", refinement, kind, lines[i])
			}
		}
	}
}
