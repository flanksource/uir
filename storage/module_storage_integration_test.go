package storage_test

import (
	"context"
	"path/filepath"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("module root storage", func() {
	It("applies child file deltas over a primary snapshot", func(ctx context.Context) {
		database := openDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "deltas.db")})
		now := time.Now().UTC()
		root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/service", Name: "service", CreatedAt: now}
		location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: "/workspace/service", Kind: "git", CreatedAt: now}
		base := storage.ModuleSnapshot{ID: uuid.New(), RootID: root.ID, LocationID: location.ID, State: storage.SnapshotReady, Revision: "main", StartedAt: now}
		child := storage.ModuleSnapshot{ID: uuid.New(), RootID: root.ID, LocationID: location.ID, BaseSnapshotID: &base.ID, State: storage.SnapshotReady, Revision: "feature", StartedAt: now}
		Expect(database.Create(&root).Error).To(Succeed())
		Expect(database.Create(&location).Error).To(Succeed())
		Expect(database.Create(&base).Error).To(Succeed())
		Expect(database.Create(&child).Error).To(Succeed())
		original := storage.SourceRevision{ID: uuid.New(), RootID: root.ID, PathKey: "service.go", ContentHash: "original", PackagePath: root.RootKey, ExtractorVersion: "v1", Projection: storage.JSON(`{}`)}
		changed := storage.SourceRevision{ID: uuid.New(), RootID: root.ID, PathKey: "service.go", ContentHash: "changed", PackagePath: root.RootKey, ExtractorVersion: "v1", Projection: storage.JSON(`{}`)}
		deleted := storage.SourceRevision{ID: uuid.New(), RootID: root.ID, PathKey: "obsolete.go", ContentHash: "obsolete", PackagePath: root.RootKey, ExtractorVersion: "v1", Projection: storage.JSON(`{}`)}
		Expect(database.Create(&original).Error).To(Succeed())
		Expect(database.Create(&changed).Error).To(Succeed())
		Expect(database.Create(&deleted).Error).To(Succeed())
		for _, delta := range []storage.SourceDelta{
			{SnapshotID: base.ID, RootID: root.ID, PathKey: original.PathKey, RevisionID: &original.ID, Operation: storage.SourceSet},
			{SnapshotID: base.ID, RootID: root.ID, PathKey: deleted.PathKey, RevisionID: &deleted.ID, Operation: storage.SourceSet},
			{SnapshotID: child.ID, RootID: root.ID, PathKey: changed.PathKey, RevisionID: &changed.ID, Operation: storage.SourceSet},
			{SnapshotID: child.ID, RootID: root.ID, PathKey: deleted.PathKey, Operation: storage.SourceDelete},
		} {
			Expect(database.Create(&delta).Error).To(Succeed())
		}
		sources, err := storage.EffectiveSources(ctx, database, child.ID)
		Expect(err).ToNot(HaveOccurred())
		Expect(sources).To(HaveKeyWithValue("service.go", changed))
		Expect(sources).ToNot(HaveKey("obsolete.go"))
	})

	It("keeps two checkout heads beneath one stable module root", func(ctx context.Context) {
		database := openDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "modules.db")})
		now := time.Now().UTC()
		root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/service", Name: "service", CreatedAt: now}
		Expect(database.Create(&root).Error).To(Succeed())
		primary := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: "/workspace/service", Kind: "git", CreatedAt: now}
		worktree := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: "/workspace/service-feature", Kind: "worktree", CreatedAt: now}
		Expect(database.Create(&primary).Error).To(Succeed())
		Expect(database.Create(&worktree).Error).To(Succeed())
		first := storage.ModuleSnapshot{ID: uuid.New(), RootID: root.ID, LocationID: primary.ID, State: storage.SnapshotReady, Revision: "main", StartedAt: now}
		second := storage.ModuleSnapshot{ID: uuid.New(), RootID: root.ID, LocationID: worktree.ID, BaseSnapshotID: &first.ID, State: storage.SnapshotReady, Revision: "feature", StartedAt: now}
		Expect(database.Create(&first).Error).To(Succeed())
		Expect(database.Create(&second).Error).To(Succeed())
		Expect(database.Create(&storage.ModuleLocationHead{RootID: root.ID, LocationID: primary.ID, SnapshotID: first.ID, Version: 1}).Error).To(Succeed())
		Expect(database.Create(&storage.ModuleLocationHead{RootID: root.ID, LocationID: worktree.ID, SnapshotID: second.ID, Version: 1}).Error).To(Succeed())
		Expect(database.Create(&storage.ModulePrimary{RootID: root.ID, LocationID: primary.ID}).Error).To(Succeed())

		var heads []storage.ModuleLocationHead
		Expect(database.Where("root_id = ?", root.ID).Order("location_id").Find(&heads).Error).To(Succeed())
		Expect(heads).To(HaveLen(2))
		var selected storage.ModulePrimary
		Expect(database.Where("root_id = ?", root.ID).First(&selected).Error).To(Succeed())
		Expect(selected.LocationID).To(Equal(primary.ID))

		locations, err := storage.ModuleLocations(ctx, database, root.RootKey)
		Expect(err).ToNot(HaveOccurred())
		Expect(locations).To(Equal([]storage.ModuleLocationView{
			{ID: primary.ID, RootKey: root.RootKey, CanonicalPath: primary.CanonicalPath, Kind: primary.Kind, Primary: true, HeadSnapshotID: first.ID, HeadVersion: 1},
			{ID: worktree.ID, RootKey: root.RootKey, CanonicalPath: worktree.CanonicalPath, Kind: worktree.Kind, HeadSnapshotID: second.ID, HeadVersion: 1},
		}))
		snapshots, total, err := storage.ModuleSnapshots(ctx, database, storage.ModuleSnapshotListOptions{
			RootKey: root.RootKey, Location: worktree.CanonicalPath, Limit: 10,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(total).To(Equal(int64(1)))
		Expect(snapshots).To(Equal([]storage.ModuleSnapshotView{{
			ID: second.ID, RootKey: root.RootKey, CanonicalPath: worktree.CanonicalPath,
			BaseSnapshotID: &first.ID, State: storage.SnapshotReady, Revision: "feature",
			StartedAt: now, Head: true, HeadVersion: 1,
		}}))
		_, _, err = storage.ModuleSnapshots(ctx, database, storage.ModuleSnapshotListOptions{
			RootKey: root.RootKey, Location: primary.CanonicalPath, Limit: -1,
		})
		Expect(err).To(MatchError(ContainSubstring("limit")))
	})
})
