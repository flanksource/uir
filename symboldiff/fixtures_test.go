package symboldiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func sqliteOptions() storage.DBOptions {
	return storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "diff.db")}
}

func postgresOptions() storage.DBOptions {
	return storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_symboldiff"}).DSN(), Schema: "uir_symboldiff"}
}

func openDatabase(ctx context.Context, options storage.DBOptions) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, options)
	Expect(err).To(Succeed())
	DeferCleanup(func() {
		sqlDB, err := database.DB()
		Expect(err).To(Succeed())
		Expect(sqlDB.Close()).To(Succeed())
	})
	return database
}

// repository is a Git checkout of one module whose commits have fixed authors and dates, so its
// commit hashes are reproducible.
type repository struct {
	path    string
	commits int
}

func newRepository() *repository {
	GinkgoHelper()
	path, err := filepath.EvalSymlinks(GinkgoT().TempDir())
	Expect(err).To(Succeed())
	repo := &repository{path: path}
	repo.git("init", "--quiet", "--initial-branch=main")
	return repo
}

func (repo *repository) git(args ...string) string {
	GinkgoHelper()
	command := exec.Command("git", append([]string{"-C", repo.path}, args...)...)
	date := "2026-01-0" + string(rune('1'+repo.commits%9)) + "T12:00:00Z"
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.org", "GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.org", "GIT_COMMITTER_DATE="+date,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	)
	output, err := command.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "git %v: %s", args, output)
	return strings.TrimSpace(string(output))
}

// write replaces the checkout's files: a nil content deletes the file.
func (repo *repository) write(files map[string]*string) {
	GinkgoHelper()
	for path, content := range files {
		absolute := filepath.Join(repo.path, filepath.FromSlash(path))
		if content == nil {
			Expect(os.Remove(absolute)).To(Succeed())
			continue
		}
		Expect(os.MkdirAll(filepath.Dir(absolute), 0o755)).To(Succeed())
		Expect(os.WriteFile(absolute, []byte(*content), 0o644)).To(Succeed())
	}
}

// commit stages every file, commits, and returns the commit hash.
func (repo *repository) commit(message string) string {
	GinkgoHelper()
	repo.git("add", "--all")
	repo.git("commit", "--quiet", "--message", message)
	repo.commits++
	return repo.git("rev-parse", "HEAD")
}

// index indexes the checkout's working tree and returns the published snapshot id.
func (repo *repository) index(ctx context.Context, database *gorm.DB) string {
	GinkgoHelper()
	engine, err := indexer.New(database)
	Expect(err).ToNot(HaveOccurred())
	results, err := engine.IndexModules(ctx, indexer.ModuleOptions{Path: repo.path})
	Expect(err).ToNot(HaveOccurred())
	Expect(results).To(HaveLen(1))
	return results[0].SnapshotID
}

// numstat is Git's own "+added -removed" for one path between two commits: an independent oracle.
func (repo *repository) numstat(from, to, path string) LineCount {
	GinkgoHelper()
	fields := strings.Fields(repo.git("diff", "--numstat", from, to, "--", path))
	Expect(fields).To(HaveLen(3), "numstat of %s", path)
	added, err := strconv.Atoi(fields[0])
	Expect(err).To(Succeed())
	removed, err := strconv.Atoi(fields[1])
	Expect(err).To(Succeed())
	return LineCount{Added: added, Removed: removed}
}

func text(value string) *string { return &value }

func lines(parts ...string) string { return strings.Join(parts, "\n") + "\n" }
