package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"time"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func publishedSnapshot(location storage.ModuleLocation, base *uuid.UUID, revision string, packages int, now time.Time) storage.ModuleSnapshot {
	return storage.ModuleSnapshot{
		ID: uuid.New(), RootID: location.RootID, LocationID: location.ID, BaseSnapshotID: base,
		Revision: revision, WorktreeState: storage.WorktreeClean, ContentSetHash: digest("content " + revision),
		ConfigurationHash: digest("configuration"), ContextHash: digest("context " + revision),
		Coverage: storage.CoverageSyntax, PackageCount: packages, Diagnostics: storage.JSON(`[]`),
		StartedAt: now, CompletedAt: now,
	}
}

func sourceRevision(root storage.ModuleRoot, path, content string) storage.SourceRevision {
	return storage.SourceRevision{ID: uuid.New(), RootID: root.ID, PathKey: path, ContentHash: digest(content), PackagePath: root.RootKey, SizeBytes: int64(len(content))}
}

func syntaxDocument(revision storage.SourceRevision, inputHash string) storage.Document {
	return storage.Document{
		ID: uuid.New(), RootID: revision.RootID, PathKey: revision.PathKey, SourceRevisionID: revision.ID,
		PackagePath: revision.PackagePath, InputHash: inputHash, IndexerVersion: "test", Coverage: storage.CoverageSyntax,
		Content: storage.JSON(`{"version":1}`),
	}
}

var _ = Describe("module root storage", func() {
	It("applies child file deltas over a primary snapshot", func(ctx context.Context) {
		database := openDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "deltas.db")})
		now := time.Now().UTC()
		root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/service", Name: "service", CreatedAt: now}
		location := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: "/workspace/service", Kind: "git", CreatedAt: now}
		base := publishedSnapshot(location, nil, "main", 1, now)
		child := publishedSnapshot(location, &base.ID, "feature", 1, now)
		for _, row := range []any{&root, &location, &base, &child} {
			Expect(database.Create(row).Error).To(Succeed())
		}
		original := sourceRevision(root, "service.go", "original")
		changed := sourceRevision(root, "service.go", "changed")
		deleted := sourceRevision(root, "obsolete.go", "obsolete")
		for _, revision := range []*storage.SourceRevision{&original, &changed, &deleted} {
			Expect(database.Create(revision).Error).To(Succeed())
		}
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
		Expect(sources).To(Equal(map[string]storage.SourceRevision{"service.go": changed}))
		mismatched := storage.SourceDelta{SnapshotID: child.ID, RootID: root.ID, PathKey: "renamed.go", RevisionID: &original.ID, Operation: storage.SourceSet}
		Expect(database.Create(&mismatched).Error).To(HaveOccurred(), "a delta's revision must belong to the delta's path")
	})

	It("keeps two checkout heads beneath one stable module root", func(ctx context.Context) {
		database := openDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "modules.db")})
		now := time.Now().UTC()
		root := storage.ModuleRoot{ID: uuid.New(), RootKey: "example.org/service", Name: "service", CreatedAt: now}
		Expect(database.Create(&root).Error).To(Succeed())
		primary := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: "/workspace/service", Kind: "git", CreatedAt: now}
		worktree := storage.ModuleLocation{ID: uuid.New(), RootID: root.ID, CanonicalPath: "/workspace/service-feature", Kind: "git", CreatedAt: now}
		Expect(database.Create(&primary).Error).To(Succeed())
		Expect(database.Create(&worktree).Error).To(Succeed())
		first := publishedSnapshot(primary, nil, "main", 0, now)
		second := publishedSnapshot(worktree, &first.ID, "feature", 0, now)
		Expect(database.Create(&first).Error).To(Succeed())
		Expect(database.Create(&second).Error).To(Succeed())
		foreign := storage.ModuleLocationHead{RootID: root.ID, LocationID: worktree.ID, SnapshotID: first.ID, Version: 1}
		Expect(database.Create(&foreign).Error).To(HaveOccurred(), "a head must reference a snapshot of its own location")
		Expect(database.Create(&storage.ModuleLocationHead{RootID: root.ID, LocationID: primary.ID, SnapshotID: first.ID, Version: 1}).Error).To(Succeed())
		Expect(database.Create(&storage.ModuleLocationHead{RootID: root.ID, LocationID: worktree.ID, SnapshotID: second.ID, Version: 1}).Error).To(Succeed())
		Expect(database.Create(&storage.ModulePrimary{RootID: root.ID, LocationID: primary.ID}).Error).To(Succeed())

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
			BaseSnapshotID: &first.ID, Revision: "feature", WorktreeState: storage.WorktreeClean, Coverage: storage.CoverageSyntax,
			StartedAt: now, CompletedAt: now, Head: true, HeadVersion: 1,
		}}))
		_, _, err = storage.ModuleSnapshots(ctx, database, storage.ModuleSnapshotListOptions{
			RootKey: root.RootKey, Location: primary.CanonicalPath, Limit: -1,
		})
		Expect(err).To(MatchError(ContainSubstring("limit")))
	})
})
