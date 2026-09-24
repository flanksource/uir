package uir

import (
	"bytes"
	"cmp"
	"encoding/json"
	"os/exec"
	"slices"
	"testing"
)

// TestPythonRefinedStatementsDecode checks the Python encoder writes a refined
// statement Type the way the Go registry does — the registered kind under
// statement_type, the refinement under statement_refinement — so each decodes
// back to the Type Python built it with.
func TestPythonRefinedStatementsDecode(t *testing.T) {
	output, err := exec.Command("python3", "python/refinement_fixture.py").Output()
	if err != nil {
		t.Fatalf("python/refinement_fixture.py: %v", err)
	}
	var cases []struct {
		Want      StatementType   `json:"want"`
		Statement json.RawMessage `json:"statement"`
	}
	if err := json.Unmarshal(output, &cases); err != nil {
		t.Fatalf("decode fixture output: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture produced no refined statements")
	}
	for _, c := range cases {
		stmt, err := StatementMarshaler.UnmarshalByType(c.Statement)
		if err != nil {
			t.Errorf("%s: decode %s: %v", c.Want, c.Statement, err)
			continue
		}
		reencoded, err := MarshalStatement(stmt)
		if err != nil {
			t.Errorf("%s: re-encode: %v", c.Want, err)
			continue
		}
		var typed struct {
			Kind       StatementType `json:"statement_type"`
			Refinement StatementType `json:"statement_refinement"`
		}
		if err := json.Unmarshal(reencoded, &typed); err != nil {
			t.Fatalf("%s: read re-encoded type: %v", c.Want, err)
		}
		if got := cmp.Or(typed.Refinement, typed.Kind); got != c.Want {
			t.Errorf("decoded %s as %q, want %q", c.Statement, got, c.Want)
		}
	}
}

// refinementProbes are refinements beyond the registered kinds: ones below a
// registered kind, ones below a kind only Go models (assignment:object), and ones
// no registered kind prefixes.
var refinementProbes = []string{
	"call:package", "call:api:http", "assignment:object:shorthand", "control:loop", "decl", "bogus", "",
}

// TestPythonRefinementVerdictsMatchGo checks Python's encoder accepts exactly the
// statement refinements Go's registry accepts, for every registered kind against
// every registered kind and each probe, so neither side writes a statement the
// other refuses to read.
func TestPythonRefinementVerdictsMatchGo(t *testing.T) {
	type pair struct {
		Kind       string `json:"kind"`
		Refinement string `json:"refinement"`
	}
	kinds := StatementMarshaler.Kinds()
	var pairs []pair
	for _, kind := range kinds {
		for _, refinement := range append(slices.Clone(kinds), refinementProbes...) {
			pairs = append(pairs, pair{kind, refinement})
		}
	}
	input, err := json.Marshal(pairs)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "python/refinement_fixture.py", "verdicts")
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python/refinement_fixture.py verdicts: %v", err)
	}
	var accepted []bool
	if err := json.Unmarshal(output, &accepted); err != nil {
		t.Fatalf("decode verdicts %s: %v", output, err)
	}
	if len(accepted) != len(pairs) {
		t.Fatalf("python returned %d verdicts for %d pairs", len(accepted), len(pairs))
	}
	for i, p := range pairs {
		goAccepts := StatementMarshaler.checkRefinement(p.Kind, p.Refinement) == nil
		if accepted[i] != goAccepts {
			t.Errorf("refining %q to %q: python accepts=%v, go accepts=%v", p.Kind, p.Refinement, accepted[i], goAccepts)
		}
	}
}
