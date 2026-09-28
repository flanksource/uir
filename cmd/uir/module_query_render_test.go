package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("query symbol rendering", func() {
	It("accepts ordered grouping levels and rejects duplicates and unknown levels", func() {
		groups, err := parseQueryGroupBy("")
		Expect(err).NotTo(HaveOccurred())
		Expect(groups).To(Equal([]string{"module", "package", "file"}))
		groups, err = parseQueryGroupBy("file,module")
		Expect(err).NotTo(HaveOccurred())
		Expect(groups).To(Equal([]string{"file", "module"}))
		for _, input := range []string{"module,module", "module,other", "module,,file"} {
			_, err = parseQueryGroupBy(input)
			Expect(err).To(HaveOccurred(), input)
		}
	})

	It("groups distinct source rows into an IDE-style symbol tree", func() {
		line, column := 12, 8
		result := moduleQueryResult{Operation: query.OperationIncoming, Total: 3, groupBy: []string{"module", "package", "file"}, Matches: []moduleQueryRow{
			{Root: "example.org/service", PackagePath: "example.org/service/api", Location: "/checkout/a", SnapshotID: "snap-a", Path: "api/handlers.go", Source: "api/handlers.go:12:8", Line: &line, Column: &column, Kind: "method", Symbol: "method:example.org/service/api:Handler.Run#()", Role: "call"},
			{Root: "example.org/service", PackagePath: "example.org/service/api", Location: "/checkout/a", SnapshotID: "snap-a", Path: "api/handlers.go", Source: "api/handlers.go:18:8", Kind: "method", Symbol: "method:example.org/service/api:Handler.Run#()", Role: "call"},
			{Root: "example.org/service", PackagePath: "example.org/service/api", Location: "/checkout/b", SnapshotID: "snap-b", Path: "api/handlers.go", Source: "api/handlers.go:12:8", Kind: "method", Symbol: "method:example.org/service/api:Handler.Run#()", Role: "call"},
		}}
		tree := moduleQueryDisplay{result: result, rows: result.Matches}.Tree()
		Expect(tree).NotTo(BeNil())
		Expect(tree.GetChildren()).To(HaveLen(2), "checkouts and snapshots must not merge")
		packageNode := tree.GetChildren()[0].GetChildren()[0]
		Expect(packageNode.Pretty().String()).To(ContainSubstring("api"))
		Expect(packageNode.Pretty().String()).NotTo(ContainSubstring("example.org/service/"))
		fileNode := packageNode.GetChildren()[0]
		Expect(fileNode.Pretty().String()).To(ContainSubstring("handlers.go"))
		Expect(fileNode.Pretty().String()).NotTo(ContainSubstring("api/handlers.go"))
		Expect(fileNode.GetChildren()).To(HaveLen(1), "repeated matches share the same symbol")
		symbol := fileNode.GetChildren()[0]
		Expect(symbol.Pretty().String()).To(And(ContainSubstring("Run"), ContainSubstring("ƒ")))
		Expect(symbol.Pretty().HTML()).To(ContainSubstring("text-purple-500"))
		Expect(symbol.GetChildren()).To(HaveLen(2))
		Expect(symbol.GetChildren()[0].Pretty().String()).To(ContainSubstring("12:8"))
		Expect(symbol.GetChildren()[1].Pretty().String()).To(ContainSubstring("18:8"))
	})

	It("collapses the root package under its module while retaining subpackages", func() {
		result := moduleQueryResult{Total: 2, groupBy: []string{"module", "package", "file"}, Matches: []moduleQueryRow{
			{Root: "example.org/service", PackagePath: "example.org/service", Path: "service.go", Symbol: "function:Root"},
			{Root: "example.org/service", PackagePath: "example.org/service/api", Path: "api/handler.go", Symbol: "function:Handle"},
		}}
		module := moduleQueryDisplay{result: result}.Tree().GetChildren()[0]
		Expect(module.GetChildren()).To(HaveLen(2))
		Expect(module.GetChildren()[0].Pretty().String()).To(ContainSubstring("service.go"))
		Expect(module.GetChildren()[0].GetChildren()[0].Pretty().String()).To(ContainSubstring("Root"))
		Expect(module.GetChildren()[1].Pretty().String()).To(ContainSubstring("api"))
		Expect(module.GetChildren()[1].GetChildren()[0].Pretty().String()).To(ContainSubstring("handler.go"))
	})

	It("renders selected module and package nodes without synthetic file leaves", func() {
		result := moduleQueryResult{Total: 2, groupBy: []string{"module", "package", "file"}, Matches: []moduleQueryRow{
			{Kind: "module", Root: "example.org/service", PackagePath: "example.org/service", Path: "example.org/service", Symbol: "module:example.org/service"},
			{Kind: "package", Root: "example.org/service", PackagePath: "example.org/service/api", Path: "example.org/service/api", Symbol: "package:example.org/service/api"},
		}}
		module := moduleQueryDisplay{result: result}.Tree().GetChildren()[0]
		Expect(module.Pretty().String()).To(ContainSubstring("example.org/service"))
		Expect(module.GetChildren()).To(HaveLen(1))
		Expect(module.GetChildren()[0].Pretty().String()).To(ContainSubstring("api"))
		Expect(module.GetChildren()[0].GetChildren()).To(BeEmpty())
	})

	It("shows declaration code without repeating a same-line definition", func() {
		definitionLine, occurrenceLine := 9, 12
		variableLine := 59
		result := moduleQueryResult{Total: 2, groupBy: []string{"module", "package", "file"}, Matches: []moduleQueryRow{{
			Root: "example.org/service", PackagePath: "example.org/service/api", Path: "api/handlers.go", Source: "api/handlers.go:12:8",
			Kind: "func", Symbol: "function:Run", Role: "call", Line: &occurrenceLine, declaration: "func Run(value int) error", definitionLine: &definitionLine, usage: "return Run(input)",
		}, {
			Root: "example.org/service", PackagePath: "example.org/service/api", Path: "api/handlers.go", Source: "api/handlers.go:59:5",
			Kind: "var", Symbol: "variable:Exec", Role: "definition", Line: &variableLine, declaration: "var Exec func(cmd string, args ...string) *exec.Process", definitionLine: &variableLine, usage: "var Exec = exec.NewExec",
		}}}
		file := moduleQueryDisplay{result: result, rows: result.Matches}.Tree().GetChildren()[0].GetChildren()[0].GetChildren()[0]
		symbol := file.GetChildren()[0]
		Expect(symbol.Pretty().String()).To(ContainSubstring("func Run(value int) error"))
		Expect(symbol.Pretty().String()).To(ContainSubstring("9"))
		Expect(symbol.Pretty().HTML()).To(And(ContainSubstring("chroma"), ContainSubstring("text-muted")))
		Expect(symbol.GetChildren()[0].Pretty().String()).To(And(ContainSubstring("12"), ContainSubstring("return Run(input)")))
		Expect(symbol.GetChildren()[0].Pretty().String()).NotTo(ContainSubstring("api/handlers.go"))
		Expect(symbol.GetChildren()[0].Pretty().HTML()).To(And(ContainSubstring("text-muted"), ContainSubstring("chroma")))
		Expect(file.GetChildren()[1].Pretty().String()).To(And(ContainSubstring("59"), ContainSubstring("var Exec func(")))
		Expect(file.GetChildren()[1].GetChildren()).To(BeEmpty())
		Expect(moduleQueryDisplay{result: result}.String()).NotTo(ContainSubstring("exec.NewExec"))
	})

	It("retains the source leaf when a definition starts on another line", func() {
		definitionLine, occurrenceLine := 58, 59
		result := moduleQueryResult{Total: 1, groupBy: []string{"file"}, Matches: []moduleQueryRow{{
			Root: "example.org/service", Path: "service.go", Kind: "var", Symbol: "variable:Exec", Role: "definition",
			Line: &occurrenceLine, definitionLine: &definitionLine, declaration: "var Exec func()", usage: "Exec = exec.NewExec",
		}}}
		symbol := moduleQueryDisplay{result: result}.Tree().GetChildren()[0].GetChildren()[0]
		Expect(symbol.GetChildren()).To(HaveLen(1))
		Expect(symbol.GetChildren()[0].Pretty().String()).To(And(ContainSubstring("59"), ContainSubstring("exec.NewExec")))
	})

	It("renders a type usage as source code after its muted line number", func() {
		line := 57
		result := moduleQueryResult{Total: 1, groupBy: []string{"file"}, Matches: []moduleQueryRow{{
			Root: "example.org/service", Path: "context/context.go", Source: "context/context.go:57:27", Line: &line,
			Kind: "type", Role: "type", Symbol: "type:Context", usage: "ctx context.Context",
		}}}
		usage := moduleQueryDisplay{result: result, rows: result.Matches}.Tree().GetChildren()[0].GetChildren()[0].GetChildren()[0].Pretty()
		Expect(usage.String()).To(And(ContainSubstring("57"), ContainSubstring("ctx context.Context")))
		Expect(usage.String()).NotTo(Or(ContainSubstring("context/context.go:57:27"), ContainSubstring(" type")))
		Expect(usage.HTML()).To(And(ContainSubstring("text-muted"), ContainSubstring("chroma")))
	})

	It("honors grouping order and keeps machine rows unchanged", func() {
		result := moduleQueryResult{Total: 1, groupBy: []string{"file", "module"}, Matches: []moduleQueryRow{{Root: "example.org/service", Location: "/checkout", SnapshotID: "snap", Path: "api/a.go", Source: "api/a.go:4", Kind: "func", Symbol: "function:example.org/service/api:Run#()"}}}
		display := moduleQueryDisplay{result: result, rows: result.Matches}
		tree := display.Tree()
		Expect(tree.GetChildren()[0].Pretty().String()).To(ContainSubstring("a.go"))
		Expect(tree.GetChildren()[0].GetChildren()[0].Pretty().String()).To(ContainSubstring("example.org/service"))
		encoded, err := json.Marshal(display)
		Expect(err).NotTo(HaveOccurred())
		var rows []moduleQueryRow
		Expect(json.Unmarshal(encoded, &rows)).To(Succeed())
		Expect(rows).To(Equal(result.Matches))
		for _, format := range []string{"pretty", "tree", "markdown", "html"} {
			output, err := clicky.Format(display, clicky.FormatOptions{Format: format, NoColor: true})
			Expect(err).NotTo(HaveOccurred(), format)
			Expect(output).To(ContainSubstring("Run"), format)
		}
		for _, format := range []string{"json", "yaml"} {
			output, err := clicky.Format(display, clicky.FormatOptions{Format: format})
			Expect(err).NotTo(HaveOccurred(), format)
			Expect(output).To(ContainSubstring("example.org/service"), format)
			Expect(output).NotTo(ContainSubstring("children"), format)
		}
		for _, format := range []string{"pretty", "tree", "markdown", "html"} {
			cliOutput, err := clicky.Format(result, clicky.FormatOptions{Format: format, NoColor: true})
			Expect(err).NotTo(HaveOccurred(), format)
			Expect(cliOutput).To(And(ContainSubstring("a.go"), ContainSubstring("Run")), format)
			Expect(cliOutput).NotTo(ContainSubstring("Operation:"), format)
		}
		cliJSON, err := clicky.Format(result, clicky.FormatOptions{Format: "json"})
		Expect(err).NotTo(HaveOccurred())
		Expect(cliJSON).To(And(ContainSubstring(`"matches"`), ContainSubstring(`"operation"`)))
	})

	It("shows empty results, incomplete coverage, and missing-head warnings", func() {
		result := moduleQueryResult{groupBy: []string{"module", "package", "file"}, Coverage: []query.ModuleCoverage{{RootKey: "example.org/service", PackagePath: "example.org/service/api", Coverage: "partial"}}, Warnings: []query.MissingHeadWarning{{RootKey: "example.org/other", Location: "/checkout/other", Message: "not indexed"}}}
		output := moduleQueryDisplay{result: result, rows: result.Matches}.Tree().Pretty().String()
		Expect(output).To(ContainSubstring("0 matches"))
		tree := moduleQueryDisplay{result: result, rows: result.Matches}.Tree()
		labels := []string{}
		for _, child := range tree.GetChildren() {
			labels = append(labels, child.Pretty().String())
		}
		Expect(strings.Join(labels, " ")).To(And(ContainSubstring("partial"), ContainSubstring("not indexed")))
	})

	It("keeps checkouts identifiable when module grouping is omitted and shows truncation", func() {
		result := moduleQueryResult{Total: 3, groupBy: []string{"file"}, Matches: []moduleQueryRow{
			{Root: "example.org/service", Location: "/checkout/a", SnapshotID: "snapshot-a", Path: "api/a.go", Symbol: "method:Run"},
			{Root: "example.org/service", Location: "/checkout/b", SnapshotID: "snapshot-b", Path: "api/a.go", Symbol: "method:Run"},
		}}
		tree := moduleQueryDisplay{result: result, rows: result.Matches}.Tree()
		Expect(tree.Pretty().String()).To(Equal("2 of 3 matches"))
		Expect(tree.GetChildren()).To(HaveLen(2))
		Expect(tree.GetChildren()[0].Pretty().String()).To(ContainSubstring("/checkout/a"))
		Expect(tree.GetChildren()[1].Pretty().String()).To(ContainSubstring("/checkout/b"))
	})

	It("renders call paths in hop order", func() {
		result := moduleQueryResult{Operation: query.OperationPath, Total: 1, Path: &moduleCallPath{Symbols: []query.ModuleSymbol{{Kind: "func", Name: "Start", QueryName: "pkg.Start"}, {Kind: "method", Name: "Finish", QueryName: "pkg.Finish"}}, Calls: []moduleQueryRow{{Source: "a.go:4:2"}}}}
		tree := moduleQueryDisplay{result: result, rows: []map[string]any{{"path": "pkg.Start -> pkg.Finish", "hops": 1}}}.Tree()
		Expect(tree.GetChildren()).To(HaveLen(1))
		Expect(tree.GetChildren()[0].Pretty().String()).To(ContainSubstring("Start"))
		Expect(tree.GetChildren()[0].GetChildren()[0].Pretty().String()).To(ContainSubstring("Finish"))
	})

	It("keeps the HTTP query envelope with grouping enabled", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		_, err := addModules(ctx, database, writeReferencesModule(), false)
		Expect(err).NotTo(HaveOccurred())
		queried, err := queryModules(ctx, database, moduleQueryOptions{Expression: "func:Save", RootKey: referencesRoot})
		Expect(err).NotTo(HaveOccurred())
		Expect(queried.Matches).NotTo(BeEmpty())
		Expect(queried.Matches[0].declaration).To(ContainSubstring("Save()"))
		Expect(queried.Matches[0].definitionLine).NotTo(BeNil())
		incoming, err := queryModules(ctx, database, moduleQueryOptions{Expression: "store.Store.Save <", RootKey: referencesRoot})
		Expect(err).NotTo(HaveOccurred())
		Expect(incoming.Matches).To(HaveLen(1))
		Expect(incoming.Matches[0].usage).To(Equal("func Run() { store.Store{}.Save() }"))
		runtime := &commandRuntime{database: database}
		handler, err := newServeHandler(newRootCommand(runtime), runtime, http.NotFoundHandler())
		Expect(err).NotTo(HaveOccurred())
		response := httptest.NewRecorder()
		body, err := json.Marshal(map[string]any{"args": []string{"func:Save"}, "root": referencesRoot, "group-by": "file,module"})
		Expect(err).NotTo(HaveOccurred())
		request := httptest.NewRequest(http.MethodPost, "/api/v1/modules/query", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(response, request)
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		var result map[string]any
		Expect(json.Unmarshal(response.Body.Bytes(), &result)).To(Succeed())
		Expect(result).To(HaveKey("matches"))
		Expect(result).To(HaveKey("coverage"))
		Expect(result).To(HaveKey("total"))
		Expect(result).NotTo(HaveKey("groupBy"))
	})
})
