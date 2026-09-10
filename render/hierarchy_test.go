package render

import (
	"strings"
	"testing"

	"github.com/flanksource/uir"
)

func TestBuildHierarchyTreeRendersStructureAndAttachments(t *testing.T) {
	runMethod := uir.MethodNode{}
	runMethod.Method = "run"

	claimReview := uir.NewType("ClaimReview").WithPackage("prototype.rules").Build()
	claimReview.Methods = []uir.MethodNode{runMethod}

	u := &uir.UIR{
		Types: []uir.TypedNode{claimReview},
		Hierarchy: &uir.HierarchyGraph{
			Nodes: []uir.HierarchyNode{
				{
					ID:          "company",
					Kind:        "company",
					DisplayName: "Prototype",
					Children: []uir.HierarchyEdge{
						{Name: "products", TargetID: "products", EdgeKind: "containment", Order: 1},
						{Name: "lookup", TargetID: "transaction", EdgeKind: "reference", Order: 2},
					},
				},
				{
					ID:          "products",
					Kind:        "collection",
					DisplayName: "products",
					Children: []uir.HierarchyEdge{
						{Name: "ClaimReview", TargetID: "transaction", EdgeKind: "containment", Order: 1},
					},
				},
				{
					ID:          "transaction",
					Kind:        "transaction",
					DisplayName: "ClaimReview",
					Attachments: []uir.HierarchyAttachment{
						{
							Role: "type",
							SymbolRef: uir.UIRSymbolRef{
								SymbolKind: "class",
								Package:    "prototype.rules",
								Type:       "ClaimReview",
								Name:       "ClaimReview",
							},
						},
					},
					Values: map[string]uir.TypedValue{
						"name": {Str: ptrStr("ClaimReview")},
					},
				},
			},
		},
	}

	tree := BuildHierarchyTree(u)
	root := tree.Pretty().String()
	if !strings.Contains(root, "hierarchy") {
		t.Fatalf("root label = %q, want hierarchy summary", root)
	}

	children := tree.GetChildren()
	if len(children) != 1 {
		t.Fatalf("root children = %d, want 1", len(children))
	}

	company := children[0]
	if got := company.Pretty().String(); !strings.Contains(got, "Prototype") {
		t.Fatalf("company label = %q", got)
	}

	companyChildren := company.GetChildren()
	if len(companyChildren) < 2 {
		t.Fatalf("company child count = %d, want at least 2", len(companyChildren))
	}

	transaction := companyChildren[0].GetChildren()[0]
	if got := transaction.Pretty().String(); !strings.Contains(got, "ClaimReview") {
		t.Fatalf("transaction label = %q", got)
	}

	transactionChildren := transaction.GetChildren()
	if len(transactionChildren) == 0 {
		t.Fatal("transaction should expose attachment or metadata children")
	}

	attached := transactionChildren[0].Pretty().String()
	if !strings.Contains(attached, "attach") || !strings.Contains(attached, "ClaimReview") {
		t.Fatalf("attachment label = %q", attached)
	}

	reference := companyChildren[1].Pretty().String()
	if !strings.Contains(reference, "ref") || !strings.Contains(reference, "lookup") {
		t.Fatalf("reference label = %q", reference)
	}
}

func ptrStr(s string) *string { return &s }
