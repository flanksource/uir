package query_test

import (
	"context"
	"path/filepath"
	"time"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = Describe("query resolution pipeline", func() {
	DescribeTable("resolves scoped symbols and call edges",
		func(ctx SpecContext, backend string) {
			database := openQueryDatabase(ctx, backend)
			fixture := seedQueryDatabase(ctx, database)
			pipeline, err := query.NewPipeline(database)
			Expect(err).ToNot(HaveOccurred())

			result, err := pipeline.Run(ctx,
				`nodes where module = "billing" and method = "Approve"`,
				query.ScopeOptions{ProjectKey: fixture.project.ProjectKey},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(nodeIDs(result.Nodes)).To(ConsistOf(fixture.target.ID, fixture.shadowTarget.ID))
			Expect(stageNames(result.Stages)).To(Equal([]string{"parse", "project", "snapshot", "root", "execute"}))

			_, err = pipeline.Run(ctx,
				`callers of node where module = "billing" and method = "Approve"`,
				query.ScopeOptions{ProjectKey: fixture.project.ProjectKey},
			)
			Expect(err).To(MatchError(ContainSubstring("matched 2 nodes")))

			result, err = pipeline.Run(ctx,
				`callers of node where root = "app" and module = "billing" and method = "Approve"`,
				query.ScopeOptions{ProjectKey: fixture.project.ProjectKey},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Target.ID).To(Equal(fixture.target.ID))
			Expect(nodeIDs(result.Nodes)).To(ConsistOf(fixture.localCaller.ID, fixture.crossRootCaller.ID))
			Expect(stageNames(result.Stages)).To(Equal([]string{"parse", "project", "snapshot", "root", "target", "execute"}))

			result, err = pipeline.Run(ctx,
				`callees of node where root = "app" and identity_key = "method|Approve"`,
				query.ScopeOptions{SnapshotID: fixture.snapshot.ID.String()},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(nodeIDs(result.Nodes)).To(Equal([]uuid.UUID{fixture.callee.ID}))

			result, err = pipeline.Run(ctx,
				`unresolved calls where root = "app"`,
				query.ScopeOptions{ProjectKey: fixture.project.ProjectKey},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(relationshipIDs(result.Relationships)).To(Equal([]uuid.UUID{fixture.unresolved.ID}))

			_, err = pipeline.Run(ctx, "nodes", query.ScopeOptions{ProjectKey: "missing"})
			Expect(err).To(MatchError(ContainSubstring(`project "missing" was not found`)))

			_, err = pipeline.Run(ctx, "nodes", query.ScopeOptions{})
			Expect(err).To(MatchError(ContainSubstring("project key or snapshot ID is required")))

			_, err = pipeline.Run(ctx, "nodes", query.ScopeOptions{ProjectKey: fixture.project.ProjectKey, Limit: 1001})
			Expect(err).To(MatchError(ContainSubstring("limit must be between 1 and 1000")))

			_, err = pipeline.Run(ctx,
				`callers of node where root = "app"`,
				query.ScopeOptions{ProjectKey: fixture.project.ProjectKey},
			)
			Expect(err).To(MatchError(ContainSubstring("at least one symbol predicate")))

			_, err = pipeline.Run(ctx,
				`nodes where root = "vendor"`,
				query.ScopeOptions{ProjectKey: fixture.project.ProjectKey, RootKey: "app"},
			)
			Expect(err).To(MatchError(ContainSubstring("conflicts")))

			_, err = pipeline.Run(ctx,
				`nodes where root = ""`,
				query.ScopeOptions{ProjectKey: fixture.project.ProjectKey},
			)
			Expect(err).To(MatchError(ContainSubstring("root predicate cannot be empty")))
		},
		Entry("SQLite", "sqlite"),
		Entry("PostgreSQL", "postgres"),
	)

	It("rejects a nil database", func() {
		pipeline, err := query.NewPipeline(nil)
		Expect(pipeline).To(BeNil())
		Expect(err).To(MatchError("UIR query database is required"))
	})
})

type queryFixture struct {
	project         storage.Project
	snapshot        storage.Snapshot
	target          storage.Node
	shadowTarget    storage.Node
	localCaller     storage.Node
	crossRootCaller storage.Node
	callee          storage.Node
	unresolved      storage.Relationship
}

func openQueryDatabase(ctx context.Context, backend string) *gorm.DB {
	GinkgoHelper()
	options := storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "query.db")}
	if backend == "postgres" {
		options.DSN = dbtest.ForGinkgo(dbtest.Options{Name: "uir_query"}).DSN()
	}
	database, err := storage.UirDB(ctx, options)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		sqlDB, dbErr := database.DB()
		Expect(dbErr).ToNot(HaveOccurred())
		Expect(sqlDB.Close()).To(Succeed())
	})
	return database
}

func seedQueryDatabase(ctx context.Context, database *gorm.DB) queryFixture {
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
	Expect(database.WithContext(ctx).Create(&project).Error).ToNot(HaveOccurred())
	Expect(database.WithContext(ctx).Create(&snapshot).Error).ToNot(HaveOccurred())
	Expect(database.WithContext(ctx).Create(&storage.ProjectHead{
		ProjectID: project.ID, SnapshotID: snapshot.ID, Version: 1, ActivatedAt: now,
	}).Error).ToNot(HaveOccurred())

	app := newQueryRoot(snapshot.ID, "app", "")
	vendor := newQueryRoot(snapshot.ID, "vendor", "vendor/example")
	Expect(database.WithContext(ctx).Create(&app).Error).ToNot(HaveOccurred())
	Expect(database.WithContext(ctx).Create(&vendor).Error).ToNot(HaveOccurred())

	target := newQueryNode(snapshot.ID, app.ID, "method|Approve", "method:billing.invoices.InvoiceService:Approve", "Approve", now)
	shadowTarget := newQueryNode(snapshot.ID, vendor.ID, "method|Approve", target.SymbolKey, "Approve", now)
	localCaller := newQueryNode(snapshot.ID, app.ID, "method|LocalCaller", "method:billing.jobs.Worker:Run", "Run", now)
	crossRootCaller := newQueryNode(snapshot.ID, vendor.ID, "method|CrossRootCaller", "method:vendor.jobs.Worker:Run", "Run", now)
	callee := newQueryNode(snapshot.ID, vendor.ID, "method|Charge", "method:payments.gateway.Client:Charge", "Charge", now)
	for _, node := range []storage.Node{target, shadowTarget, localCaller, crossRootCaller, callee} {
		Expect(database.WithContext(ctx).Create(&node).Error).ToNot(HaveOccurred())
	}

	resolved := []storage.Relationship{
		newCall(snapshot.ID, localCaller, target, app.RootKey, "call|local"),
		newCall(snapshot.ID, crossRootCaller, target, app.RootKey, "call|cross-root"),
		newCall(snapshot.ID, target, callee, vendor.RootKey, "call|callee"),
	}
	for _, relationship := range resolved {
		Expect(database.WithContext(ctx).Create(&relationship).Error).ToNot(HaveOccurred())
	}
	unresolved := storage.Relationship{
		ID: uuid.New(), SnapshotID: snapshot.ID, FromRootID: app.ID, FromNodeID: localCaller.ID,
		EdgeKey: "call|external", ToProjectKey: stringPointer("external"), ToRootKey: stringPointer("service"),
		ToIdentityKey: "method|Missing", ToSymbolKey: "method:external.Service:Missing",
		ToIdentifier:     storage.JSON(`{"module":"external","type":"Service","method":"Missing","node_type":"method"}`),
		RelationshipType: "call", StatementPath: "body/1", Text: "Missing", Payload: storage.JSON(`{}`),
	}
	Expect(database.WithContext(ctx).Create(&unresolved).Error).ToNot(HaveOccurred())
	return queryFixture{project, snapshot, target, shadowTarget, localCaller, crossRootCaller, callee, unresolved}
}

func newQueryRoot(snapshotID uuid.UUID, key, mount string) storage.Root {
	return storage.Root{
		ID: uuid.New(), SnapshotID: snapshotID, RootKey: key, Kind: "git", MountPath: mount,
		ContentSetHash: key + "-hash", PathCase: "sensitive", NormalizationVersion: "v1", Properties: storage.JSON(`{}`),
	}
}

func newQueryNode(snapshotID, rootID uuid.UUID, identity, symbol, method string, createdAt time.Time) storage.Node {
	return storage.Node{
		ID: uuid.New(), SnapshotID: snapshotID, RootID: rootID, ChildSlot: "methods", NodeType: "method",
		IdentityKey: identity, SymbolKey: symbol, Module: "billing", Package: "invoices", TypeName: "InvoiceService",
		Method: method, Language: "go", Traits: storage.JSON(`[]`), PayloadSchema: "v1", Payload: storage.JSON(`{}`),
		SemanticHash: identity + "-hash", CreatedAt: createdAt,
	}
}

func newCall(snapshotID uuid.UUID, from, to storage.Node, rootKey, edgeKey string) storage.Relationship {
	return storage.Relationship{
		ID: uuid.New(), SnapshotID: snapshotID, FromRootID: from.RootID, FromNodeID: from.ID,
		ToSnapshotID: &snapshotID, ToNodeID: &to.ID, EdgeKey: edgeKey, ToRootKey: &rootKey,
		ToIdentityKey: to.IdentityKey, ToSymbolKey: to.SymbolKey, ToIdentifier: storage.JSON(`{}`),
		RelationshipType: "call", StatementPath: "body/0", Text: to.Method, Payload: storage.JSON(`{}`),
	}
}

func nodeIDs(nodes []storage.Node) []uuid.UUID {
	ids := make([]uuid.UUID, len(nodes))
	for index := range nodes {
		ids[index] = nodes[index].ID
	}
	return ids
}

func relationshipIDs(relationships []storage.Relationship) []uuid.UUID {
	ids := make([]uuid.UUID, len(relationships))
	for index := range relationships {
		ids[index] = relationships[index].ID
	}
	return ids
}

func stageNames(stages []query.ResolutionStage) []string {
	names := make([]string, len(stages))
	for index := range stages {
		names[index] = stages[index].Name
	}
	return names
}

func stringPointer(value string) *string {
	return &value
}
