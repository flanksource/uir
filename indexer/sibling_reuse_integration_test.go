package indexer

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	rpchttp "github.com/flanksource/clicky/rpc/http"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"
)

const (
	siblingLibrary = "package lib\n\nconst Rate = %s\n\nfunc Base() int { return helper() }\n\nfunc helper() int { return %s }\n"
	siblingApp     = "package app\n\nimport \"example.org/lib\"\n\nfunc Total(items int) int { return items * lib.Base() * lib.Rate }\n"
)

// commitCheckout commits every change in the repository at checkout.
func commitCheckout(ctx context.Context, checkout, message string) {
	GinkgoHelper()
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "-m", message}} {
		output, err := exec.CommandContext(ctx, "git", append([]string{"-C", checkout}, args...)...).CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(output))
	}
}

// runTypechecks runs one module-index run over paths, in that order, and returns how often it
// type-checked a module.
func runTypechecks(ctx context.Context, engine *Indexer, paths ...string) int64 {
	GinkgoHelper()
	requests := make([]ModuleOptions, 0, len(paths))
	for _, path := range paths {
		requests = append(requests, ModuleOptions{Path: path, Reason: storage.ReasonAdd})
	}
	run, err := StartModulesTask(ctx, engine, requests)
	Expect(err).ToNot(HaveOccurred())
	waiting, timings := rpchttp.WithTimings(ctx)
	_, err = run.Wait(waiting)
	Expect(err).ToNot(HaveOccurred())
	calls, _ := timings.Counter("typecheck", "calls")
	return calls
}

// writeCheckouts writes each module as its own committed git checkout below workspace, with a go.work
// that uses them all.
func writeCheckouts(ctx context.Context, workspace string, modules map[string][2]string) {
	GinkgoHelper()
	work := "go 1.26\n\nuse (\n"
	for _, name := range sortedKeys(modules) {
		work += "\t./" + name + "\n"
		writeModule(filepath.Join(workspace, name), "example.org/"+name, modules[name][0], modules[name][1])
		output, err := exec.CommandContext(ctx, "git", "-C", filepath.Join(workspace, name), "init").CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(output))
		commitCheckout(ctx, filepath.Join(workspace, name), "test: checkout")
	}
	writeFile(filepath.Join(workspace, "go.work"), work+")\n")
}

var _ = Describe("workspace sibling confirmation", func() {
	It("confirms a sibling outside the run whose own local dependency is unchanged", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeCheckouts(ctx, workspace, map[string][2]string{
			"base": {"", "package base\n\nfunc Base() int { return 1 }\n"},
			"lib": {"\nrequire example.org/base v0.0.0\n\nreplace example.org/base => ../base\n",
				"package lib\n\nimport \"example.org/base\"\n\nfunc Price() int { return base.Base() }\n"},
			"app": {"", "package app\n\nimport \"example.org/lib\"\n\nfunc Total() int { return lib.Price() }\n"},
		})
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		app := filepath.Join(workspace, "app")
		Expect(runTypechecks(ctx, engine, app, filepath.Join(workspace, "lib"), filepath.Join(workspace, "base"))).To(Equal(int64(3)))
		Expect(runTypechecks(ctx, engine, app)).To(BeZero(), "lib is confirmed through its head, linked to base's confirmed head")
	})

	It("confirms a sibling indexed under another go.work by the sources of every workspace package the load read", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		other := filepath.Join(workspace, "other")
		writeModule(filepath.Join(other, "core"), "example.org/core", "", "package core\n\nfunc Rate() int { return 2 }\n")
		writeModule(filepath.Join(other, "lib"), "example.org/lib", "",
			"package lib\n\nimport \"example.org/core\"\n\nfunc Price() int { return helper() * core.Rate() }\n\nfunc helper() int { return 10 }\n")
		writeFile(filepath.Join(other, "go.work"), "go 1.26\n\nuse (\n\t./core\n\t./lib\n)\n")
		writeCheckouts(ctx, workspace, map[string][2]string{
			"app": {"", "package app\n\nimport \"example.org/lib\"\n\nfunc Total() int { return lib.Price() }\n"},
		})
		writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse (\n\t./app\n\t./other/core\n\t./other/lib\n)\n")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		app := filepath.Join(workspace, "app")
		Expect(runTypechecks(ctx, engine, app)).To(Equal(int64(1)))
		Expect(runTypechecks(ctx, engine, app)).To(BeZero(), "lib builds under other/go.work, and no sibling source changed")

		writeFile(filepath.Join(other, "lib", "main.go"),
			"package lib\n\nimport \"example.org/core\"\n\nfunc Price() int { return helper() * core.Rate() }\n\nfunc helper() int { return 11 }\n")
		Expect(runTypechecks(ctx, engine, app)).To(Equal(int64(1)), "a changed sibling source, even an unexported body, type-checks app")
		Expect(runTypechecks(ctx, engine, app)).To(BeZero())

		writeFile(filepath.Join(other, "core", "main.go"), "package core\n\nfunc Rate() int { return 3 }\n")
		Expect(runTypechecks(ctx, engine, app)).To(Equal(int64(1)), "a changed source of a package lib imports type-checks app")
	})

	It("reuses a module whose only sibling imports are in files no variant compiles", func(ctx SpecContext) {
		database := openIndexerDB(ctx, indexerSQLiteOptions())
		workspace := canonicalTempDir()
		writeCheckouts(ctx, workspace, map[string][2]string{
			"lib": {"", "package lib\n\nfunc Price() int { return 1 }\n"},
			"app": {"", "package app\n\nfunc Total() int { return 1 }\n"},
		})
		writeFile(filepath.Join(workspace, "app", "ignored.go"), "//go:build ignore\n\npackage app\n\nimport \"example.org/lib\"\n\nvar _ = lib.Price\n")
		commitCheckout(ctx, filepath.Join(workspace, "app"), "test: ignored sibling import")
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		paths := []string{filepath.Join(workspace, "lib"), filepath.Join(workspace, "app")}
		Expect(runTypechecks(ctx, engine, paths...)).To(Equal(int64(2)))
		Expect(runTypechecks(ctx, engine, paths...)).To(BeZero(), "app recorded lib as imported by no compiled file")
	})
})

var _ = Describe("workspace sibling reuse", func() {
	var database *gorm.DB
	var workspace string
	var typechecks func(context.Context) int64
	writeLibrary := func(rate, helper string) {
		writeFile(filepath.Join(workspace, "lib", "main.go"), fmt.Sprintf(siblingLibrary, rate, helper))
	}

	BeforeEach(func(ctx SpecContext) {
		database = openIndexerDB(ctx, indexerSQLiteOptions())
		workspace = canonicalTempDir()
		writeModule(filepath.Join(workspace, "app"), "example.org/app", "", siblingApp)
		writeModule(filepath.Join(workspace, "lib"), "example.org/lib", "", "")
		writeLibrary("2", "10")
		writeFile(filepath.Join(workspace, "go.work"), "go 1.26\n\nuse (\n\t./app\n\t./lib\n)\n")
		for _, module := range []string{"app", "lib"} {
			output, err := exec.CommandContext(ctx, "git", "-C", filepath.Join(workspace, module), "init").CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(output))
			commitCheckout(ctx, filepath.Join(workspace, module), "test: sibling checkout")
		}
		engine, err := New(database)
		Expect(err).ToNot(HaveOccurred())
		typechecks = func(ctx context.Context) int64 {
			GinkgoHelper()
			return runTypechecks(ctx, engine, filepath.Join(workspace, "app"), filepath.Join(workspace, "lib"))
		}
		Expect(typechecks(ctx)).To(Equal(int64(2)))
	})

	It("reuses an importer's head while its sibling's export shape is unchanged", func(ctx SpecContext) {
		Expect(typechecks(ctx)).To(BeZero(), "an unchanged run type-checks neither module")

		writeLibrary("2", "11")
		commitCheckout(ctx, filepath.Join(workspace, "lib"), "test: change an unexported helper")
		Expect(typechecks(ctx)).To(Equal(int64(1)), "only lib is type-checked; app's recorded shape of lib still holds")

		writeLibrary("3", "11")
		commitCheckout(ctx, filepath.Join(workspace, "lib"), "test: change an exported constant")
		Expect(typechecks(ctx)).To(Equal(int64(2)), "a changed export shape type-checks app too")
		Expect(typechecks(ctx)).To(BeZero())
	})

	It("type-checks an importer whose head recorded no sibling shapes, and records them", func(ctx SpecContext) {
		Expect(database.Where("1 = 1").Delete(&storage.SnapshotSiblingShape{}).Error).To(Succeed())
		Expect(typechecks(ctx)).To(Equal(int64(1)), "app's head predates sibling shapes")
		Expect(countRows(database, &storage.SnapshotSiblingShape{}, "import_path = ?", "example.org/lib")).To(Equal(int64(1)))
		Expect(typechecks(ctx)).To(BeZero(), "the unchanged extraction recorded the head's sibling shapes")
	})
})
