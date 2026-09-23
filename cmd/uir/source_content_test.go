package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

var _ = Describe("snapshot source content", func() {
	It("reads a local source ref without comparing its saved hash", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		root, source := seededSource(ctx, database)
		directory := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(directory, source.PathKey), []byte("current local text"), 0o644)).To(Succeed())
		root.LocalPath = &directory
		Expect(database.Save(&root).Error).To(Succeed())

		content, err := readSourceContent(withDatabase(ctx, database), source.ID.String(), sourceContentOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(content.Origin).To(Equal("local"))
		Expect(content.Content).To(Equal("current local text"))
	})
	It("reads a directory root locally even when its parent Git revision was recorded", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		root, source := seededSource(ctx, database)
		directory := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(directory, source.PathKey), []byte("directory text"), 0o644)).To(Succeed())
		revision := strings.Repeat("a", 40)
		root.Kind, root.LocalPath, root.Revision = "directory", &directory, &revision
		Expect(database.Save(&root).Error).To(Succeed())

		content, err := readSourceContent(withDatabase(ctx, database), source.ID.String(), sourceContentOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(content.Origin).To(Equal("local"))
		Expect(content.Content).To(Equal("directory text"))
	})

	It("reads the recorded Git revision after the worktree changes", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		root, source := seededSource(ctx, database)
		directory := GinkgoT().TempDir()
		runGit(directory, "init", "-q")
		runGit(directory, "config", "user.email", "test@example.com")
		runGit(directory, "config", "user.name", "Test User")
		path := filepath.Join(directory, source.PathKey)
		Expect(os.WriteFile(path, []byte("saved revision text"), 0o644)).To(Succeed())
		runGit(directory, "add", source.PathKey)
		runGit(directory, "commit", "-qm", "add source")
		revision := runGit(directory, "rev-parse", "HEAD")
		root.LocalPath, root.Revision = &directory, &revision
		Expect(database.Save(&root).Error).To(Succeed())
		Expect(os.WriteFile(path, []byte("changed worktree text"), 0o644)).To(Succeed())

		content, err := readSourceContent(withDatabase(ctx, database), source.ID.String(), sourceContentOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(content.Origin).To(Equal("git"))
		Expect(content.Content).To(Equal("saved revision text"))
	})

	It("returns a remote ref and rejects a source path outside its local root", func(ctx SpecContext) {
		database := openCommandDatabase(ctx)
		seedQueryProject(ctx, database)
		root, source := seededSource(ctx, database)
		repository, revision := "https://example.com/acme/repo.git", "abc123"
		root.RepositoryURI, root.Revision = &repository, &revision
		Expect(database.Save(&root).Error).To(Succeed())
		content, err := readSourceContent(withDatabase(ctx, database), source.ID.String(), sourceContentOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(content.Origin).To(Equal("remote"))
		Expect(content.Repository).To(Equal(repository))

		directory := GinkgoT().TempDir()
		root.LocalPath = &directory
		root.Revision = nil
		Expect(database.Save(&root).Error).To(Succeed())
		outside := filepath.Join(GinkgoT().TempDir(), "secret.go")
		Expect(os.WriteFile(outside, []byte("secret"), 0o644)).To(Succeed())
		Expect(os.Symlink(outside, filepath.Join(directory, source.PathKey))).To(Succeed())
		_, err = readSourceContent(withDatabase(ctx, database), source.ID.String(), sourceContentOptions{})
		Expect(err).To(MatchError(ContainSubstring("outside root")))
	})
})

func seededSource(ctx context.Context, database *gorm.DB) (storage.Root, storage.Source) {
	GinkgoHelper()
	var root storage.Root
	Expect(database.WithContext(ctx).First(&root).Error).To(Succeed())
	var source storage.Source
	Expect(database.WithContext(ctx).First(&source).Error).To(Succeed())
	return root, source
}

func runGit(directory string, args ...string) string {
	GinkgoHelper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(output))
	return strings.TrimSpace(string(output))
}
