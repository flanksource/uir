package query_test

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	appModule = "example.org/app"
	appRun    = appModule + ".Run"
	appSource = `package app

import (
	"fmt"
	"strings"

	"example.org/app/audit"
	"example.org/lib/greet"
	"example.org/lib/shout"
	"gorm.io/gorm"
)

func Run(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("no names")
	}
	message := greet.Hello(strings.Join(names, ", "))
	fmt.Println(shout.Loud(message))
	audit.Log(message)
	return gorm.Open(message)
}
`
	appGoMod = "module " + appModule + `

go 1.26

require (
	example.org/lib v0.0.0
	gorm.io/gorm v0.0.0
)

replace example.org/lib => ../lib

replace gorm.io/gorm => ../gorm
`
)

// appWorkspace writes a module whose Run calls the standard library, a builtin, a package of its
// own, two packages of a local library module, and a local stand-in for gorm.io/gorm. Only the app
// module is indexed, so the library and gorm packages are outside the query scope.
func appWorkspace() string {
	GinkgoHelper()
	workspace := GinkgoT().TempDir()
	for path, content := range map[string]string{
		"app/go.mod":         appGoMod,
		"app/app.go":         appSource,
		"app/audit/audit.go": "package audit\n\nfunc Log(string) {}\n",
		"lib/go.mod":         "module example.org/lib\n\ngo 1.26\n",
		"lib/greet/greet.go": "package greet\n\nfunc Hello(name string) string { return \"hello \" + name }\n",
		"lib/shout/shout.go": "package shout\n\nfunc Loud(text string) string { return text + \"!\" }\n",
		"gorm/go.mod":        "module gorm.io/gorm\n\ngo 1.26\n",
		"gorm/gorm.go":       "package gorm\n\nfunc Open(dsn string) error { return nil }\n",
	} {
		Expect(os.MkdirAll(filepath.Dir(filepath.Join(workspace, path)), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, path), []byte(content), 0o644)).To(Succeed())
	}
	return filepath.Join(workspace, "app")
}

var _ = Describe("call graph package exclusions", func() {
	var (
		pipeline *query.Pipeline
		scope    query.ModuleScopeOptions
	)

	BeforeEach(func(ctx SpecContext) {
		database := openQueryDatabase(ctx, "sqlite")
		checkout := appWorkspace()
		indexCheckout(ctx, database, checkout)
		var err error
		pipeline, err = query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		scope = query.ModuleScopeOptions{RootKey: appModule, Location: checkout}
	})

	runGraph := func(ctx SpecContext, selector string, direction graph.Direction, exclude []string) query.GraphResult {
		GinkgoHelper()
		return callGraph(ctx, pipeline, query.GraphOptions{Selector: selector, Direction: direction, Depth: 1, Exclude: exclude, Scope: scope})
	}

	DescribeTable("draws the packages the patterns leave in and tallies the nodes they leave out",
		func(ctx SpecContext, exclude, effective, labels []string, excluded map[string]int) {
			result := runGraph(ctx, appRun, graph.DirectionCallees, exclude)
			Expect(result.Exclude).To(Equal(effective))
			Expect(graphLabels(result)).To(Equal(labels))
			Expect(result.Omitted.Excluded).To(Equal(excluded))
		},
		Entry("by default, the standard library, builtins and gorm",
			nil, []string{"std", "builtin", "gorm.io/..."},
			[]string{"Hello", "Log", "Loud", "Run"},
			map[string]int{"fmt": 2, "strings": 1, "builtin": 1, "gorm.io/gorm": 1}),
		Entry("nothing for none",
			[]string{"none"}, []string{"none"},
			[]string{"Errorf", "Hello", "Join", "Log", "Loud", "Open", "Println", "Run", "len"},
			nil),
		Entry("every package outside the modules in scope for external",
			[]string{"external"}, []string{"external"},
			[]string{"Log", "Run"},
			map[string]int{"fmt": 2, "strings": 1, "builtin": 1, "gorm.io/gorm": 1, "example.org/lib/greet": 1, "example.org/lib/shout": 1}),
		Entry("a package and every package below it for a /... pattern, in place of the defaults",
			[]string{"example.org/lib/..."}, []string{"example.org/lib/..."},
			[]string{"Errorf", "Join", "Log", "Open", "Println", "Run", "len"},
			map[string]int{"example.org/lib/greet": 1, "example.org/lib/shout": 1}),
		Entry("only the named packages for exact paths",
			[]string{"example.org/lib/greet", "fmt"}, []string{"example.org/lib/greet", "fmt"},
			[]string{"Join", "Log", "Loud", "Open", "Run", "len"},
			map[string]int{"example.org/lib/greet": 1, "fmt": 2}),
		Entry("never the root, even when its package is excluded",
			[]string{"example.org/app/..."}, []string{"example.org/app/..."},
			[]string{"Errorf", "Hello", "Join", "Loud", "Open", "Println", "Run", "len"},
			map[string]int{"example.org/app/audit": 1}),
	)

	It("lists every package reached, drawn or excluded, by node count with its external and excluded flags", func(ctx SpecContext) {
		Expect(runGraph(ctx, appRun, graph.DirectionCallees, nil).Packages).To(Equal([]query.GraphPackage{
			{Path: "fmt", External: true, Nodes: 2, Excluded: true},
			{Path: "builtin", External: true, Nodes: 1, Excluded: true},
			{Path: appModule, Nodes: 1},
			{Path: appModule + "/audit", Nodes: 1},
			{Path: "example.org/lib/greet", External: true, Nodes: 1},
			{Path: "example.org/lib/shout", External: true, Nodes: 1},
			{Path: "gorm.io/gorm", External: true, Nodes: 1, Excluded: true},
			{Path: "strings", External: true, Nodes: 1, Excluded: true},
		}))
		Expect(runGraph(ctx, appRun, graph.DirectionCallees, []string{"none"}).Packages).To(ContainElement(
			query.GraphPackage{Path: "fmt", External: true, Nodes: 2},
		))
	})

	It("counts a root's own package when the patterns exclude it", func(ctx SpecContext) {
		result := runGraph(ctx, appModule+"/audit.Log", graph.DirectionCallers, []string{appModule + "/audit"})
		Expect(graphLabels(result)).To(Equal([]string{"Log", "Run"}))
		Expect(result.Omitted.Excluded).To(BeNil())
		Expect(result.Packages).To(Equal([]query.GraphPackage{
			{Path: appModule, Nodes: 1},
			{Path: appModule + "/audit", Nodes: 1, Excluded: true},
		}))
	})

	It("echoes the patterns and returns empty packages for an ambiguous selector", func(ctx SpecContext) {
		result := callGraph(ctx, pipeline, query.GraphOptions{Selector: "func:Run | func:Log", Exclude: []string{"external"}, Scope: scope})
		Expect(result.Candidates).To(HaveLen(2))
		Expect(result.Exclude).To(Equal([]string{"external"}))
		Expect(result.Packages).To(Equal([]query.GraphPackage{}))
	})

	DescribeTable("rejects a pattern it cannot match",
		func(ctx SpecContext, exclude []string, message string) {
			_, err := pipeline.Graph(ctx, query.GraphOptions{Selector: appRun, Exclude: exclude, Scope: scope})
			var invalid *query.InvalidQueryError
			Expect(errors.As(err, &invalid)).To(BeTrue(), "%v is an invalid query", err)
			Expect(invalid.Message).To(ContainSubstring(message))
			Expect(invalid.Hint).To(ContainSubstring("/..."))
		},
		Entry("none beside another pattern", []string{"none", "std"}, "none excludes nothing"),
		Entry("an empty pattern", []string{"std", ""}, "empty"),
		Entry("a glob", []string{"gorm.io/*"}, `"gorm.io/*"`),
		Entry("a wildcard before the end", []string{"gorm.io/.../driver"}, `"gorm.io/.../driver"`),
		Entry("a bare wildcard", []string{"..."}, `"..."`),
		Entry("surrounding space", []string{" std"}, `" std"`),
		Entry("a repeated pattern", []string{"std", "std"}, `"std" is given twice`),
	)
})
