package uir_test

import (
	"fmt"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/uir"
)

// servicePackage is com.example{ UserService{ GetUser }, init }: methods at
// depth 1 (package function) and depth 2 (type method).
func servicePackage() uir.PackageNode {
	return *uir.NewPackage("com.example").
		WithFunction(uir.NewMethod("init").Build()).
		WithType(uir.NewType("UserService").WithMethod(uir.NewMethod("GetUser").Build()).Build()).
		Build()
}

func methodNames(methods []uir.MethodNode) []string {
	var names []string
	for _, m := range methods {
		names = append(names, m.GetIdentifier().GetName())
	}
	slices.Sort(names)
	return names
}

var _ = Describe("NodeTree.Walk", func() {
	It("visits every descendant below the hidden root, passing the node itself", func() {
		var visited []string
		uir.NodeTree{Node: servicePackage()}.Walk(func(node uir.Node) bool {
			visited = append(visited, fmt.Sprintf("%T:%s", node, node.GetIdentifier().GetName()))
			return true
		})

		Expect(visited).To(ConsistOf("uir.TypedNode:UserService", "uir.MethodNode:GetUser", "uir.MethodNode:init"))
	})

	It("includes the root when asked", func() {
		var visited []string
		uir.NodeTree{Node: servicePackage()}.Walk(func(node uir.Node) bool {
			visited = append(visited, fmt.Sprintf("%T", node))
			return true
		}, uir.WalkOptions{IncludeRoot: true})

		Expect(visited).To(HaveLen(4))
		Expect(visited[0]).To(Equal("uir.PackageNode"))
	})

	It("calls f on a Stop node but does not descend into it, and keeps walking its siblings", func() {
		var visited []string
		uir.NodeTree{Node: servicePackage()}.Walk(func(node uir.Node) bool {
			visited = append(visited, node.GetIdentifier().GetName())
			return true
		}, uir.WalkOptions{Stop: []uir.NodeType{uir.NodeTypeType}})

		Expect(visited).To(ConsistOf("UserService", "init"))
	})

	It("does not visit nodes deeper than Depth", func() {
		var visited []string
		uir.NodeTree{Node: servicePackage()}.Walk(func(node uir.Node) bool {
			visited = append(visited, node.GetIdentifier().GetName())
			return true
		}, uir.WalkOptions{Depth: 1})

		Expect(visited).To(ConsistOf("UserService", "init"))
	})
})

var _ = Describe("FindOptions.Many", func() {
	It("finds methods at every depth without warnings", func() {
		methods, warnings := uir.Find[uir.MethodNode](servicePackage()).Many()

		Expect(methodNames(methods)).To(Equal([]string{"GetUser", "init"}))
		Expect(warnings).To(BeEmpty())
	})

	It("returns a warning for each matching node of the wrong Go type", func() {
		methods, warnings := uir.FindOptions[uir.MethodNode]{Root: servicePackage()}.Many()

		Expect(methodNames(methods)).To(Equal([]string{"GetUser", "init"}))
		Expect(warnings).To(HaveLen(1))
		Expect(warnings[0].Node).To(BeAssignableToTypeOf(uir.TypedNode{}))
	})

	It("honours WithDepth", func() {
		methods, _ := uir.Find[uir.MethodNode](servicePackage()).WithDepth(1).Many()

		Expect(methodNames(methods)).To(Equal([]string{"init"}))
	})

	It("honours the name filter below non-matching ancestors", func() {
		method, warnings := uir.Find[uir.MethodNode](servicePackage()).WithName("GetUser").One()

		Expect(method.GetIdentifier().GetName()).To(Equal("GetUser"))
		Expect(warnings).To(BeEmpty())
	})
})

var _ = Describe("NodeTree.GroupByPackage", func() {
	It("merges same-named packages and groups package-less nodes under the empty package", func() {
		doc := uir.UIR{
			Packages: []uir.PackageNode{
				*uir.NewPackage("a").WithFunction(uir.NewMethod("one").Build()).Build(),
				*uir.NewPackage("b").WithFunction(uir.NewMethod("two").Build()).Build(),
				*uir.NewPackage("a").WithFunction(uir.NewMethod("three").Build()).Build(),
			},
			Functions: []uir.MethodNode{uir.NewMethod("main").Build()},
		}

		packages, err := uir.NodeTree{Node: doc}.GroupByPackage()

		Expect(err).NotTo(HaveOccurred())
		grouped := map[string][]string{}
		for _, pkg := range packages {
			grouped[pkg.Package] = methodNames(pkg.Functions)
		}
		Expect(grouped).To(Equal(map[string][]string{
			"a": {"one", "three"},
			"b": {"two"},
			"":  {"main"},
		}))
	})
})

func callTargets(relationships []uir.Relationship) []string {
	var targets []string
	for _, rel := range relationships {
		targets = append(targets, rel.GetTo().GetIdentifier().Method)
	}
	return targets
}

// nestedBlocks is { { save() }; load() }.
func nestedBlocks() uir.BlockStmt {
	inner := uir.BlockStmt{Children: []uir.Statement{uir.NewMethodCall("save").MethodCallStmt}}
	return uir.BlockStmt{Children: []uir.Statement{inner, uir.NewMethodCall("load").MethodCallStmt}}
}

var _ = Describe("BlockStmt.GetRelationships", func() {
	It("collects relationships from statements nested in blocks", func() {
		Expect(callTargets(nestedBlocks().GetRelationships())).To(ConsistOf("save", "load"))
	})
})

var _ = Describe("NodeTree.GetRelationships", func() {
	It("collects each relationship once from relatable nodes at any depth", func() {
		Expect(callTargets(uir.NewTree(nestedBlocks()).GetRelationships())).To(ConsistOf("save", "load"))
	})
})
