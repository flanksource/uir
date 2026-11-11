package lint

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/uir/internal/progress"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lint task progress", func() {
	It("runs selected rules in one task and reports their execution position", func(ctx SpecContext) {
		config := lintCheckout()
		Expect(os.WriteFile(config.Path, []byte(validConfig+"    - id: second\n      query: SELECT path FROM files()\n      message: '{{.path}} checked'\n"), 0o600)).To(Succeed())
		var configErr error
		config, configErr = Load(config.Path)
		Expect(configErr).NotTo(HaveOccurred())
		var mu sync.Mutex
		seen := map[string]bool{}
		ids := map[string]bool{}
		release := task.ObserveTasks(func(event task.TaskEvent) {
			snapshot := event.Snapshot()
			if strings.HasPrefix(snapshot.Name, "Linting:") {
				mu.Lock()
				seen[snapshot.Name] = true
				ids[snapshot.ID] = true
				mu.Unlock()
			}
		})
		database := lintDatabase(ctx, "sqlite")
		report := &Result{LinterResult: &linters.LinterResult{WorkDir: config.Root}}
		_, err := progress.Run(ctx, "Linting", "module-lint", func(ctx context.Context) (int, error) {
			return 0, execute(ctx, database, config, Options{}, report)
		})
		release()
		Expect(err).NotTo(HaveOccurred())
		mu.Lock()
		defer mu.Unlock()
		Expect(ids).To(HaveLen(1))
		Expect(seen).To(HaveLen(report.RuleCount))
		Expect(seen).To(HaveKey("Linting: small 1/2"))
		Expect(seen).To(HaveKey("Linting: second 2/2"))
	})
})
