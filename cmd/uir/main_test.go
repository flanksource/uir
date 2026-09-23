package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var _ = Describe("UIR entity CLI", func() {
	It("generates project list, query, and reindex commands from one entity", func() {
		root := newRootCommand(&commandRuntime{})
		project, _, err := root.Find([]string{"project"})
		Expect(err).ToNot(HaveOccurred())
		Expect(project.Runnable()).To(BeTrue())
		Expect(commandNames(project)).To(ContainElements("get", "query", "reindex"))
		Expect(clicky.GetContextDataFunc(findCommand(project, "query"))).ToNot(BeNil())
		Expect(clicky.GetContextDataFunc(findCommand(project, "reindex"))).ToNot(BeNil())
	})

	It("runs PEG queries through the entity action and returns table rows", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		root := newRootCommand(&commandRuntime{})
		queryCommand := findCommand(findCommand(root, "project"), "query")
		handler := clicky.GetContextDataFunc(queryCommand)

		result, err := handler(withDatabase(ctx, database), map[string]string{
			"expression": `nodes where method = "Run"`,
		}, []string{"acme"})
		Expect(err).ToNot(HaveOccurred())
		rows, ok := result.([]queryRow)
		Expect(ok).To(BeTrue())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Row()).To(gstruct.MatchAllKeys(gstruct.Keys{
			"id": Not(BeEmpty()),
			"operation": Equal("nodes"), "root": Equal("app"), "node_type": Equal("method"),
			"symbol": Equal("method:acme.Worker:Run"), "package": BeEmpty(), "type": Equal("Worker"),
			"method": Equal("Run"), "field": BeEmpty(), "signature": BeEmpty(), "language": Equal("go"),
			"location": BeEmpty(),
		}))
		Expect(rows[0].Columns()).ToNot(BeEmpty())
	})

	It("renders unresolved call locations from their source", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		queryCommand := findCommand(findCommand(newRootCommand(&commandRuntime{}), "project"), "query")

		result, err := clicky.GetContextDataFunc(queryCommand)(withDatabase(ctx, database), map[string]string{
			"expression": "unresolved calls",
		}, []string{"acme"})
		Expect(err).ToNot(HaveOccurred())
		rows, ok := result.([]queryRow)
		Expect(ok).To(BeTrue())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0].Location).To(Equal("worker.go:12:4"))
	})

	It("starts incremental indexing from the reindex action", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		workspace := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.org/cli\n\ngo 1.26\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package cli\n\nfunc Run() {}\n"), 0o644)).To(Succeed())
		root := newRootCommand(&commandRuntime{})
		reindexCommand := findCommand(findCommand(root, "project"), "reindex")
		handler := clicky.GetContextDataFunc(reindexCommand)

		result, err := handler(withDatabase(ctx, database), map[string]string{
			"path": workspace, "root": "workspace",
		}, []string{"cli-project"})
		Expect(err).ToNot(HaveOccurred())
		indexed, ok := result.(indexer.Result)
		Expect(ok).To(BeTrue())
		Expect(indexed.ProjectKey).To(Equal("cli-project"))
		Expect(indexed.ParsedFiles).To(Equal(1))
		Expect(indexed.HeadVersion).To(Equal(int64(1)))
	})
})

func openCommandDatabase(ctx context.Context) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "command.db")})
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		sqlDB, dbErr := database.DB()
		Expect(dbErr).ToNot(HaveOccurred())
		Expect(sqlDB.Close()).To(Succeed())
	})
	return database
}

func seedQueryProject(ctx context.Context, database *gorm.DB) {
	GinkgoHelper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	project := storage.Project{
		ID: uuid.New(), ProjectKey: "acme", Name: "Acme", Properties: storage.JSON(`{}`), CreatedAt: now, UpdatedAt: now,
	}
	snapshot := storage.Snapshot{
		ID: uuid.New(), ProjectID: project.ID, State: storage.SnapshotReady,
		RevisionSetHash: "revision", ConfigurationHash: "configuration", ExtractorVersion: "v1",
		PayloadSchema: "v1", DocumentPayload: storage.JSON(`{}`), StartedAt: now, CompletedAt: &now, Properties: storage.JSON(`{}`),
	}
	root := storage.Root{
		ID: uuid.New(), SnapshotID: snapshot.ID, RootKey: "app", Kind: "git", MountPath: "",
		ContentSetHash: "content", PathCase: "sensitive", NormalizationVersion: "v1", Properties: storage.JSON(`{}`),
	}
	node := storage.Node{
		ID: uuid.New(), SnapshotID: snapshot.ID, RootID: root.ID, ChildSlot: "methods", NodeType: "method",
		IdentityKey: "method|Run", SymbolKey: "method:acme.Worker:Run", Module: "acme", TypeName: "Worker", Method: "Run",
		Language: "go", Traits: storage.JSON(`[]`), PayloadSchema: "v1", Payload: storage.JSON(`{}`),
		SemanticHash: "run-hash", CreatedAt: now,
	}
	source := storage.Source{
		ID: uuid.New(), RootID: root.ID, PathKey: "worker.go", DisplayPath: "worker.go", Kind: "file",
		Language: "go", ContentHash: "content", SizeBytes: 64, Properties: storage.JSON(`{}`),
	}
	line, column := 12, 4
	relationship := storage.Relationship{
		ID: uuid.New(), SnapshotID: snapshot.ID, FromRootID: root.ID, FromNodeID: node.ID,
		EdgeKey: "call|missing", ToIdentityKey: "function|Missing", ToSymbolKey: "function:acme:Missing",
		ToIdentifier: storage.JSON(`{}`), RelationshipType: string(uir.RelationshipTypeCall), SourceID: &source.ID,
		StartLine: &line, EndLine: &line, Column: &column, StatementPath: "body/0", Payload: storage.JSON(`{}`),
	}
	Expect(database.WithContext(ctx).Create(&project).Error).ToNot(HaveOccurred())
	Expect(database.WithContext(ctx).Create(&snapshot).Error).ToNot(HaveOccurred())
	Expect(database.WithContext(ctx).Create(&storage.ProjectHead{
		ProjectID: project.ID, SnapshotID: snapshot.ID, Version: 1, ActivatedAt: now,
	}).Error).ToNot(HaveOccurred())
	Expect(database.WithContext(ctx).Create(&root).Error).ToNot(HaveOccurred())
	Expect(database.WithContext(ctx).Create(&node).Error).ToNot(HaveOccurred())
	Expect(database.WithContext(ctx).Create(&source).Error).ToNot(HaveOccurred())
	Expect(database.WithContext(ctx).Create(&relationship).Error).ToNot(HaveOccurred())
}

func findCommand(parent *cobra.Command, name string) *cobra.Command {
	GinkgoHelper()
	for _, command := range parent.Commands() {
		if command.Name() == name {
			return command
		}
	}
	Fail("command " + name + " was not generated")
	return nil
}

func commandNames(parent *cobra.Command) []string {
	names := make([]string, 0, len(parent.Commands()))
	for _, command := range parent.Commands() {
		names = append(names, command.Name())
	}
	return names
}
