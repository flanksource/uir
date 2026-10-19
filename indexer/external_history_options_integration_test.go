package indexer

import (
	"strings"

	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("historical publication options", func() {
	It("parents a historical location on the explicitly selected shared base", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		base := prodPublication("base", "100")
		base.Options = PublishOptions{Historical: true, GitCommit: strings.Repeat("a", 40), Producer: "producer-v1"}
		baseResult := publish(ctx, database, base)

		head := prodPublication("head", "101")
		head.Location.URI = "oipa://example/pr-17"
		head.Options = PublishOptions{Historical: true, GitCommit: strings.Repeat("b", 40), Producer: "producer-v1", BaseSnapshotID: baseResult.SnapshotID}
		headResult := publish(ctx, database, head)
		var snapshot storage.ModuleSnapshot
		Expect(database.Where("id = ?", headResult.SnapshotID).First(&snapshot).Error).To(Succeed())
		Expect(snapshot.BaseSnapshotID).To(HaveValue(Equal(uuid.MustParse(baseResult.SnapshotID))))
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		source, err := pipeline.ReadModuleSource(ctx, headResult.SnapshotID, "rules/Calc.xml")
		Expect(err).ToNot(HaveOccurred())
		Expect(source.Content).To(Equal(head.Documents[1].Source))
	})

	It("records commit identity, preserves heads and invalidates reuse for a changed producer", func(ctx SpecContext) {
		database := openIndexerSQLite(ctx)
		current := publish(ctx, database, prodPublication("current", "200"))
		publication := prodPublication("old", "100")
		publication.Options = PublishOptions{Historical: true, GitCommit: strings.Repeat("a", 40), Producer: "producer-v1"}
		first := publish(ctx, database, publication)
		Expect(first.HeadVersion).To(Equal(current.HeadVersion))
		again := publish(ctx, database, publication)
		Expect(again.SnapshotID).To(Equal(first.SnapshotID))
		Expect(again.Unchanged).To(BeTrue())
		publication.Options.Producer = "producer-v2"
		changed := publish(ctx, database, publication)
		Expect(changed.SnapshotID).ToNot(Equal(first.SnapshotID))
		var snapshot storage.ModuleSnapshot
		Expect(database.Where("id = ?", first.SnapshotID).First(&snapshot).Error).To(Succeed())
		Expect(snapshot.GitCommit).To(Equal(publication.Options.GitCommit))
		Expect(snapshot.Reason).To(Equal(storage.ReasonHistorical))
		pipeline, err := query.NewPipeline(database)
		Expect(err).ToNot(HaveOccurred())
		source, err := pipeline.ReadModuleSource(ctx, first.SnapshotID, "rules/Calc.xml")
		Expect(err).ToNot(HaveOccurred())
		Expect(source.Content).To(Equal(publication.Documents[1].Source))
	})

	DescribeTable("rejects incomplete or invalid historical identity before writing",
		func(ctx SpecContext, options PublishOptions) {
			database := openIndexerSQLite(ctx)
			publication := prodPublication("old", "100")
			publication.Options = options
			_, err := Publish(ctx, database, publication)
			Expect(err).To(MatchError(ErrInvalidPublication))
			Expect(rowCounts(database, "modules", "snapshots", "documents")).To(HaveEach(BeZero()))
		},
		Entry("no commit", PublishOptions{Historical: true, Producer: "v1"}),
		Entry("no producer", PublishOptions{Historical: true, GitCommit: strings.Repeat("a", 40)}),
		Entry("unresolved ref", PublishOptions{Historical: true, GitCommit: "main", Producer: "v1"}),
	)
})
