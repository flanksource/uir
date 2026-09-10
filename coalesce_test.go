package uir

import (
	"testing"
)

func TestCoalesce_MergesTypesByPackageAndType(t *testing.T) {
	u := UIR{
		Types: []TypedNode{
			{
				nodeBase: nodeBase{
					Identifier: Identifier{Package: "policyadmin.omah", Type: "PayrollFileDataIntake"},
				},
				Methods: []MethodNode{
					{nodeBase: nodeBase{Identifier: Identifier{Method: "run"}}},
				},
				Implements: []TypeReference{{Name: "Transaction"}},
			},
			{
				nodeBase: nodeBase{
					Identifier: Identifier{Package: "policyadmin.omah", Type: "PayrollFileDataIntake"},
				},
				Methods: []MethodNode{
					{nodeBase: nodeBase{Identifier: Identifier{Method: "applyRules"}}},
				},
				Implements: []TypeReference{{Name: "TransactionBusinessRulePacket"}},
			},
			{
				nodeBase: nodeBase{
					Identifier: Identifier{Package: "policyadmin.omah", Type: "PayrollFileDataIntake"},
				},
				Methods: []MethodNode{
					{nodeBase: nodeBase{Identifier: Identifier{Method: "validate"}}},
				},
				Implements: []TypeReference{{Name: "ValidateExpressions"}},
			},
			{
				nodeBase: nodeBase{
					Identifier: Identifier{Package: "policyadmin.omah", Type: "CreateClients"},
				},
				Methods: []MethodNode{
					{nodeBase: nodeBase{Identifier: Identifier{Method: "CreateClients"}}},
				},
			},
		},
	}

	merged := u.Coalesce()

	if got, want := len(merged.Types), 2; got != want {
		t.Fatalf("Coalesce: got %d types, want %d", got, want)
	}

	var pfdi *TypedNode
	for i := range merged.Types {
		if merged.Types[i].Type == "PayrollFileDataIntake" {
			pfdi = &merged.Types[i]
		}
	}
	if pfdi == nil {
		t.Fatal("PayrollFileDataIntake not found in merged result")
	}
	if got, want := len(pfdi.Methods), 3; got != want {
		t.Errorf("merged PayrollFileDataIntake: got %d methods, want %d", got, want)
	}
	if got, want := len(pfdi.Implements), 3; got != want {
		t.Errorf("merged PayrollFileDataIntake: got %d implements, want %d", got, want)
	}

	wantImpls := map[string]bool{
		"Transaction":                   true,
		"TransactionBusinessRulePacket": true,
		"ValidateExpressions":           true,
	}
	for _, impl := range pfdi.Implements {
		if !wantImpls[impl.Name] {
			t.Errorf("unexpected implements: %s", impl.Name)
		}
		delete(wantImpls, impl.Name)
	}
	for missing := range wantImpls {
		t.Errorf("missing implements: %s", missing)
	}
}

func TestCoalesce_IdempotentOnUniqueTypes(t *testing.T) {
	u := UIR{
		Types: []TypedNode{
			{nodeBase: nodeBase{Identifier: Identifier{Package: "a", Type: "X"}}},
			{nodeBase: nodeBase{Identifier: Identifier{Package: "a", Type: "Y"}}},
			{nodeBase: nodeBase{Identifier: Identifier{Package: "b", Type: "X"}}},
		},
	}
	if got := len(u.Coalesce().Types); got != 3 {
		t.Errorf("Coalesce changed count of unique types: got %d, want 3", got)
	}
	if got := len(u.Coalesce().Coalesce().Types); got != 3 {
		t.Errorf("Coalesce not idempotent: got %d, want 3", got)
	}
}

func TestCoalesce_UnionsVariablesByName(t *testing.T) {
	u := UIR{
		Types: []TypedNode{
			{
				nodeBase: nodeBase{Identifier: Identifier{Package: "p", Type: "T"}},
				Variables: ParamsDef{
					{nodeBase: nodeBase{Identifier: Identifier{Field: "a"}}},
					{nodeBase: nodeBase{Identifier: Identifier{Field: "b"}}},
				},
			},
			{
				nodeBase: nodeBase{Identifier: Identifier{Package: "p", Type: "T"}},
				Variables: ParamsDef{
					{nodeBase: nodeBase{Identifier: Identifier{Field: "b"}}},
					{nodeBase: nodeBase{Identifier: Identifier{Field: "c"}}},
				},
			},
		},
	}
	merged := u.Coalesce()
	if got := len(merged.Types); got != 1 {
		t.Fatalf("got %d types, want 1", got)
	}
	if got := len(merged.Types[0].Variables); got != 3 {
		t.Errorf("got %d variables, want 3 (a,b,c deduped)", got)
	}
}

func TestCoalesce_PrefersTransactionRootLocationAndPreservesOutputPathHint(t *testing.T) {
	u := UIR{
		Types: []TypedNode{
			{
				nodeBase: nodeBase{
					Identifier: Identifier{Package: "policyadmin.omah", Type: "PayrollFileDataIntake"},
					Metadata: Metadata{Properties: map[string]any{
						"outputPathHint": "Example Holdings/Customer Plan/PayrollFileDataIntake.ts",
					}},
					SourceCode: SourceCode{Location: Location{
						Path: "file:///x/rules/pc/Example Holdings/pl/Customer Plan/tr/PayrollFileDataIntake/br/TransactionBusinessRulePacket/XMLData.xml",
					}},
				},
			},
			{
				nodeBase: nodeBase{
					Identifier: Identifier{Package: "policyadmin.omah", Type: "PayrollFileDataIntake"},
					SourceCode: SourceCode{Location: Location{
						Path: "file:///x/rules/pc/Example Holdings/pl/Customer Plan/tr/PayrollFileDataIntake/XMLData.xml",
					}},
				},
			},
		},
	}

	merged := u.Coalesce()
	if got, want := len(merged.Types), 1; got != want {
		t.Fatalf("got %d types, want %d", got, want)
	}

	typ := merged.Types[0]
	if got, want := typ.Path, "file:///x/rules/pc/Example Holdings/pl/Customer Plan/tr/PayrollFileDataIntake/XMLData.xml"; got != want {
		t.Fatalf("location path = %q, want %q", got, want)
	}
	if got, want := typ.Properties["outputPathHint"], "Example Holdings/Customer Plan/PayrollFileDataIntake.ts"; got != want {
		t.Fatalf("outputPathHint = %#v, want %#v", got, want)
	}
}

// Coalescing keeps both nodes' methods, so it has to keep both nodes' import
// hints — otherwise the merged type renders calls from the second node with
// only the first node's imports, and the generated file cannot compile.
func TestCoalesce_UnionsExternalImportHints(t *testing.T) {
	hint := func(value string) TypedNode {
		return TypedNode{nodeBase: nodeBase{
			Identifier: Identifier{Package: "policyadmin.transactions", Type: "Anniversary"},
			Metadata:   Metadata{Properties: map[string]any{ExternalImportProperty: value}},
		}}
	}
	u := UIR{Types: []TypedNode{
		hint("copyToPolicyFields@@@rules/pc/OMA/tr/Anniversary/br\nshared@@@rules/br"),
		hint("copyBookAnniversary@@@rules/br\nshared@@@rules/br"),
	}}

	merged := u.Coalesce()
	if got, want := len(merged.Types), 1; got != want {
		t.Fatalf("got %d types, want %d", got, want)
	}
	want := "copyToPolicyFields@@@rules/pc/OMA/tr/Anniversary/br\nshared@@@rules/br\ncopyBookAnniversary@@@rules/br"
	if got := merged.Types[0].Properties[ExternalImportProperty]; got != want {
		t.Fatalf("externalImport = %#v, want %#v", got, want)
	}
}
