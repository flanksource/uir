package query_test

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// assertBrowseContract pins the browse JSON the web explorer reads: every key of a source and of the
// Caller node, with its navigation position (the name's line and UTF-16 column) and its outgoing call.
func assertBrowseContract(result query.ModuleBrowseResult) {
	GinkgoHelper()
	encoded, err := json.Marshal(result)
	Expect(err).ToNot(HaveOccurred())
	var decoded struct {
		Sources []map[string]any `json:"sources"`
		Nodes   []map[string]any `json:"nodes"`
	}
	Expect(json.Unmarshal(encoded, &decoded)).To(Succeed())
	Expect(decoded.Sources).To(HaveLen(1))
	Expect(decoded.Sources[0]).To(HaveLen(8))
	Expect(decoded.Sources[0]).To(HaveKeyWithValue("path", "browse.go"))
	Expect(decoded.Sources[0]).To(HaveKeyWithValue("package_path", "example.org/browse"))
	sourceID := decoded.Sources[0]["id"]
	var caller map[string]any
	for _, node := range decoded.Nodes {
		if node["identifier"].(map[string]any)["method"] == "Caller" {
			caller = node
		}
	}
	Expect(caller).ToNot(BeNil())
	Expect(caller["payload"]).To(HaveKeyWithValue("method", "Caller"))
	Expect(caller["payload"]).To(HaveKeyWithValue("sourceCode", map[string]any{"path": "browse.go", "start_line": 4.0, "end_line": 4.0}))
	Expect(caller["semantic_hash"]).To(HaveLen(64))
	delete(caller, "payload")
	delete(caller, "semantic_hash")
	identity := `v1:[\"method\",\"\",\"example.org/browse\",\"\",\"Caller\",\"\",\"()\"]`
	actual, err := json.Marshal(caller)
	Expect(err).ToNot(HaveOccurred())
	Expect(actual).To(MatchJSON(`{
		"id": "` + sourceID.(string) + `:` + identity + `", "source_id": "` + sourceID.(string) + `", "path": "browse.go",
		"symbol": "method:example.org/browse:Caller#()", "node_type": "method",
		"identifier": {"package": "example.org/browse", "method": "Caller", "signature": "()", "node_type": "method"},
		"parent_identity": "v1:[\"package\",\"\",\"example.org/browse\",\"\",\"\",\"\",\"\"]",
		"child_slot": "methods", "ordinal": 1, "line": 4, "end_line": 4, "column": 6,
		"calls": [{"to_identifier": {"package": "example.org/browse", "method": "Target", "node_type": "method"},
			"resolvable": true, "statement_path": "calls/000000", "line": 4, "text": "Target"}]
	}`))
}

var _ = Describe("module browsing", func() {
	DescribeTable("reads immutable source and node projections", func(ctx SpecContext, backend string) {
		database := openQueryDatabase(ctx, backend)
		workspace := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/browse\n\ngo 1.26\n"), 0o644)).To(Succeed())
		path := filepath.Join(workspace, "browse.go")
		Expect(os.WriteFile(path, []byte("package browse\n\nfunc Target() {}\nfunc Caller() { Target() }\n"), 0o644)).To(Succeed())
		engine, err := indexer.New(database)
		Expect(err).To(Succeed())
		published, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: workspace})
		Expect(err).To(Succeed())
		pipeline, err := query.NewPipeline(database)
		Expect(err).To(Succeed())
		result, err := pipeline.BrowseModules(ctx, query.ModuleScopeOptions{RootKey: "example.org/browse", SnapshotID: published[0].SnapshotID})
		Expect(err).To(Succeed())
		Expect(result.Sources).To(HaveLen(1))
		Expect(result.Sources[0].Path).To(Equal("browse.go"))
		Expect(result.Sources[0].ContentHash).ToNot(BeEmpty())
		Expect(result.Nodes).To(ContainElement(And(HaveField("Symbol", ContainSubstring("Target")), HaveField("Path", "browse.go"))))
		Expect(result.Nodes).To(ContainElement(HaveField("Calls", HaveLen(1))))
		assertBrowseContract(result)
		content, err := pipeline.ReadModuleSource(ctx, published[0].SnapshotID, "browse.go")
		Expect(err).To(Succeed())
		Expect(content.Content).To(ContainSubstring("func Target()"))
		Expect(content.Origin).To(Equal("local"))

		Expect(os.Remove(path)).To(Succeed())
		_, err = engine.IndexModules(ctx, indexer.ModuleOptions{Path: workspace})
		Expect(err).To(Succeed())
		historical, err := pipeline.BrowseModules(ctx, query.ModuleScopeOptions{SnapshotID: published[0].SnapshotID})
		Expect(err).To(Succeed())
		Expect(historical).To(Equal(result))
		_, err = pipeline.ReadModuleSource(ctx, published[0].SnapshotID, "browse.go")
		Expect(err).To(MatchError(ContainSubstring("no pinned Git revision")))
	}, Entry("SQLite", "sqlite"), Entry("PostgreSQL", "postgres"))
})
