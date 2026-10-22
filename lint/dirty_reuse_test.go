package lint

import (
	"os"
	"path/filepath"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Dirty checkout lint reuse", func() {
	DescribeTable("reuses the saved head on repeat and invalidates it when source contents change", func(ctx SpecContext, state string) {
		root := sourceCheckout(ctx)
		const parameters = "version: 1\nlint:\n  rules:\n    - {id: parameters, query: 'SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 3', message: '{{.name}}', severity: warning}\n"
		Expect(os.WriteFile(filepath.Join(root, ConfigName), []byte(parameters), 0o600)).To(Succeed())
		sourceGit(ctx, root, "add", ConfigName)
		sourceGit(ctx, root, "commit", "-qm", "parameter rule")
		config, err := Load(filepath.Join(root, ConfigName))
		Expect(err).NotTo(HaveOccurred())
		sample := filepath.Join(root, "small.go")
		const dirtySource = "package linted\nfunc Dirty(a,b,c,d int) {}\n"
		switch state {
		case "untracked":
			sample = filepath.Join(root, "untracked.go")
		case "manifest":
			Expect(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/linted\n\ngo 1.26\n// local changes\n"), 0o600)).To(Succeed())
		}
		Expect(os.WriteFile(sample, []byte(dirtySource), 0o600)).To(Succeed())
		if state == "staged" || state == "staged and unstaged" {
			sourceGit(ctx, root, "add", "small.go")
		}
		if state == "staged and unstaged" {
			Expect(os.WriteFile(sample, []byte("package linted\nfunc Live(a,b,c,d int) {}\n"), 0o600)).To(Succeed())
		}
		before := sourceGit(ctx, root, "status", "--porcelain")
		Expect(before).NotTo(BeEmpty())
		databaseOptions := storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "lint.db")}
		database, err := storage.UirDB(ctx, databaseOptions)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			connection, err := database.DB()
			Expect(err).NotTo(HaveOccurred())
			Expect(connection.Close()).To(Succeed())
		})
		var firstHead storage.ModuleSnapshot
		for run := range 3 {
			if run == 2 {
				connection, err := database.DB()
				Expect(err).NotTo(HaveOccurred())
				Expect(connection.Close()).To(Succeed())
				database, err = storage.UirDB(ctx, databaseOptions)
				Expect(err).NotTo(HaveOccurred())
			}
			timed, timings := rpchttp.WithTimings(ctx)
			report := Run(timed, database, config, Options{})
			Expect(report.Success).To(BeTrue(), report.Error)
			name := "Dirty"
			if state == "staged and unstaged" {
				name = "Live"
			}
			Expect(report.Violations).To(ContainElement(HaveField("Message", HaveValue(Equal(name)))))
			loads, _ := timings.Counter("typecheck", "calls")
			var snapshots []storage.ModuleSnapshot
			Expect(database.Find(&snapshots).Error).To(Succeed())
			Expect(snapshots).To(HaveLen(1))
			if run == 0 {
				Expect(loads).To(BeZero(), "metric rules must not load Go packages")
				firstHead = snapshots[0]
				Expect(firstHead.WorktreeState).To(Equal(storage.WorktreeDirty))
			} else {
				Expect(loads).To(BeZero(), "a repeat must not load Go packages")
				Expect(snapshots[0].ID).To(Equal(firstHead.ID))
			}
		}
		Expect(sourceGit(ctx, root, "status", "--porcelain")).To(Equal(before))
		Expect(os.WriteFile(sample, []byte("package linted\nfunc Updated() {}\n"), 0o600)).To(Succeed())
		timed, timings := rpchttp.WithTimings(ctx)
		report := Run(timed, database, config, Options{})
		Expect(report.Success).To(BeTrue(), report.Error)
		Expect(report.Violations).NotTo(ContainElement(HaveField("File", sample)))
		loads, _ := timings.Counter("typecheck", "calls")
		Expect(loads).To(BeZero(), "changed syntax must not require Go package loading")
		var snapshots []storage.ModuleSnapshot
		Expect(database.Find(&snapshots).Error).To(Succeed())
		Expect(snapshots).To(HaveLen(2))
	},
		Entry("untracked source", "untracked"),
		Entry("unstaged source", "unstaged"),
		Entry("staged source", "staged"),
		Entry("staged and unstaged source", "staged and unstaged"),
		Entry("uncommitted manifest", "manifest"),
	)
})
