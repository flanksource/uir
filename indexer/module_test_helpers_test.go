package indexer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

func openIndexerSQLite(ctx context.Context) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "index.db")})
	Expect(err).To(Succeed())
	DeferCleanup(closeIndexerDB, database)
	return database
}

func openIndexerPostgres(ctx context.Context) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_indexer"}).DSN(), Schema: "uir_indexer"})
	Expect(err).To(Succeed())
	DeferCleanup(closeIndexerDB, database)
	return database
}

func closeIndexerDB(database *gorm.DB) {
	GinkgoHelper()
	sqlDB, err := database.DB()
	Expect(err).To(Succeed())
	Expect(sqlDB.Close()).To(Succeed())
}

// canonicalTempDir is a spec temp directory with symlinks resolved, the form the index registers
// checkouts under, so specs hold where TMPDIR is itself a symlink.
func canonicalTempDir() string {
	GinkgoHelper()
	directory, err := filepath.EvalSymlinks(GinkgoT().TempDir())
	Expect(err).ToNot(HaveOccurred())
	return directory
}

// gitOutput runs git in directory and returns its trimmed standard output.
func gitOutput(ctx context.Context, directory string, args ...string) string {
	GinkgoHelper()
	output, err := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...).Output()
	Expect(err).ToNot(HaveOccurred(), "git %v", args)
	return strings.TrimSpace(string(output))
}

// commitPaths stages paths and commits them, returning the new commit.
func commitPaths(ctx context.Context, directory, message string, paths ...string) string {
	GinkgoHelper()
	for _, args := range [][]string{append([]string{"add"}, paths...), {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", message}} {
		output, err := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...).CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(output))
	}
	return gitOutput(ctx, directory, "rev-parse", "HEAD")
}

func writeFile(path, content string) {
	GinkgoHelper()
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
	modified := time.Now().Add(time.Second)
	Expect(os.Chtimes(path, modified, modified)).To(Succeed())
}
