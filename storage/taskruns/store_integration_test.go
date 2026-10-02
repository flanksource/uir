package taskruns_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	clickytask "github.com/flanksource/clicky/task"
	"github.com/flanksource/commons-db/dbtest"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/taskruns"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	runID         = "run-0001"
	moduleIndex   = "module-index"
	firstSnapshot = "0b8d0b1e-9a6f-4b8e-8f8e-3f6a1c2d4e01"
	moduleFailure = "index module \"example.org/broken\": parse failed"
)

func openDatabase(ctx context.Context, options storage.DBOptions) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, options)
	Expect(err).To(Succeed())
	DeferCleanup(func() {
		connection, err := database.DB()
		Expect(err).To(Succeed())
		Expect(connection.Close()).To(Succeed())
	})
	return database
}

func sqliteOptions() storage.DBOptions {
	return storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "runs.db")}
}

func postgresOptions() storage.DBOptions {
	return storage.DBOptions{DSN: dbtest.ForGinkgo(dbtest.Options{Name: "uir_task_runs"}).DSN(), Schema: "uir_task_runs"}
}

// finishedRun is a failed module-index run: one module published a snapshot, one failed, and the
// failed task logged far more lines than a stored run keeps.
func finishedRun(status string) []clickytask.TaskSnapshot {
	logs := make([]clickytask.LogEntry, 0, taskruns.StoredLogEntries+50)
	for index := range taskruns.StoredLogEntries + 50 {
		logs = append(logs, clickytask.LogEntry{Level: "info", Message: fmt.Sprintf("line %d", index)})
	}
	return []clickytask.TaskSnapshot{
		{ID: "Index /repo", Name: "Index /repo", Type: "group", Status: status, GroupID: runID, Kind: moduleIndex,
			Labels: map[string]string{"path": "/repo", "reason": "add"}, StartedAt: "2026-10-01T10:00:00Z", FinishedAt: "2026-10-01T10:00:05Z",
			Total: 2, Completed: 1, Failed: 1, Details: map[string]any{"snapshot_ids": []string{firstSnapshot}}},
		{ID: "task-1", Name: "example.org/ok", Type: "task", Status: "success", GroupID: runID},
		{ID: "task-2", Name: "example.org/broken", Type: "task", Status: "failed", GroupID: runID, Error: moduleFailure,
			Logs: logs, Stdout: strings.Repeat("x", taskruns.StoredStreamBytes+10)},
	}
}

var _ = Describe("task run store", func() {
	DescribeTable("keeps a finished run, with the snapshots it published, after the process forgets it",
		func(ctx SpecContext, options func() storage.DBOptions) {
			config := options()
			database := openDatabase(ctx, config)
			store := taskruns.New(database)
			Expect(store.SaveRun(ctx, runID, finishedRun("running"))).To(Succeed())
			Expect(store.SaveRun(ctx, runID, finishedRun("failed"))).To(Succeed(), "a later save of one run replaces the earlier one")

			reopened := taskruns.New(openDatabase(ctx, config))
			runs, err := reopened.Runs(ctx, clickytask.RunFilter{Kind: moduleIndex})
			Expect(err).To(Succeed())
			Expect(runs).To(Equal([]clickytask.RunMeta{clickytask.RunMetaFromSnapshot(finishedRun("failed")[0])}))
			Expect(reopened.Runs(ctx, clickytask.RunFilter{Kind: moduleIndex, Labels: map[string]string{"reason": "reindex"}})).To(BeEmpty())
			Expect(reopened.Runs(ctx, clickytask.RunFilter{Status: "success"})).To(BeEmpty())

			snapshots, err := reopened.Snapshot(ctx, runID)
			Expect(err).To(Succeed())
			Expect(snapshots).To(HaveLen(3))
			failed := snapshots[2]
			Expect(failed.Logs).To(HaveLen(taskruns.StoredLogEntries), "a stored task keeps only its newest log entries")
			Expect(failed.Logs[len(failed.Logs)-1].Message).To(Equal(fmt.Sprintf("line %d", taskruns.StoredLogEntries+49)))
			Expect([]any{len(failed.Stdout), failed.StdoutTruncated, failed.StdoutOffset}).To(Equal([]any{taskruns.StoredStreamBytes, true, int64(10)}))

			var row taskruns.Run
			Expect(database.Where("id = ?", runID).Take(&row).Error).To(Succeed())
			var snapshotIDs []string
			Expect(json.Unmarshal(row.SnapshotIDs, &snapshotIDs)).To(Succeed())
			Expect([]any{snapshotIDs, row.Error, row.Status}).To(Equal([]any{[]string{firstSnapshot}, "example.org/broken: " + moduleFailure, "failed"}))
		},
		Entry("SQLite", sqliteOptions),
		Entry("PostgreSQL", postgresOptions),
	)

	It("answers an unknown run with no snapshots and refuses to control a stored run", func(ctx SpecContext) {
		store := taskruns.New(openDatabase(ctx, sqliteOptions()))
		Expect(store.Snapshot(ctx, "never-ran")).To(BeEmpty())
		Expect(store.SaveRun(ctx, runID, finishedRun("success"))).To(Succeed())

		Expect(store.Control(ctx, runID, clickytask.ControlStop)).To(MatchError(ContainSubstring("run " + runID + " is not running in this process")))
	})

	It("rejects a save that carries no group snapshot", func(ctx SpecContext) {
		store := taskruns.New(openDatabase(ctx, sqliteOptions()))

		Expect(store.SaveRun(ctx, runID, finishedRun("success")[1:])).To(MatchError(ContainSubstring("has no group snapshot")))
	})
})
