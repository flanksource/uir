package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/clicky"
	clickytask "github.com/flanksource/clicky/task"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var _ = Describe("UIR module CLI", func() {
	It("stores the default database under the home config directory", func(ctx SpecContext) {
		home := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		GinkgoT().Setenv(dsnEnv, "")
		GinkgoT().Setenv(schemaEnv, "")
		runtime := &commandRuntime{}
		_, err := runtime.Database(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { Expect(runtime.Close()).To(Succeed()) })
		Expect(runtime.DSN).To(Equal(filepath.Join(home, ".config", "uir", "uir.db")))
		_, err = os.Stat(runtime.DSN)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("database selection from the environment", func() {
		var home, envDSN, flagDSN string
		BeforeEach(func() {
			home = GinkgoT().TempDir()
			GinkgoT().Setenv("HOME", home)
			envDSN = filepath.Join(GinkgoT().TempDir(), "env.db")
			flagDSN = filepath.Join(GinkgoT().TempDir(), "flag.db")
			GinkgoT().Setenv(dsnEnv, envDSN)
			GinkgoT().Setenv(schemaEnv, "")
		})

		openWithArgs := func(ctx context.Context, args ...string) (*commandRuntime, error) {
			runtime := &commandRuntime{}
			Expect(newRootCommand(runtime).PersistentFlags().Parse(args)).To(Succeed())
			_, err := runtime.Database(ctx)
			DeferCleanup(func() { Expect(runtime.Close()).To(Succeed()) })
			return runtime, err
		}

		It("opens UIR_DSN when --dsn is absent instead of the home config database", func(ctx SpecContext) {
			runtime, err := openWithArgs(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(runtime.DSN).To(Equal(envDSN))
			Expect(envDSN).To(BeAnExistingFile())
			Expect(filepath.Join(home, ".config", "uir", "uir.db")).ToNot(BeAnExistingFile())
		})

		It("opens the --dsn flag over UIR_DSN", func(ctx SpecContext) {
			runtime, err := openWithArgs(ctx, "--dsn", flagDSN)
			Expect(err).ToNot(HaveOccurred())
			Expect(runtime.DSN).To(Equal(flagDSN))
			Expect(flagDSN).To(BeAnExistingFile())
			Expect(envDSN).ToNot(BeAnExistingFile())
		})

		It("passes UIR_SCHEMA to storage when --schema is absent", func(ctx SpecContext) {
			GinkgoT().Setenv(schemaEnv, "tenant_env")
			_, err := openWithArgs(ctx)
			Expect(err).To(MatchError(ContainSubstring("tenant_env")))
		})

		It("passes the --schema flag over UIR_SCHEMA", func(ctx SpecContext) {
			GinkgoT().Setenv(schemaEnv, "tenant_env")
			runtime, err := openWithArgs(ctx, "--schema", "public")
			Expect(err).ToNot(HaveOccurred())
			Expect(runtime.Schema).To(Equal("public"))
		})
	})

	It("prints the linked build version", func() {
		root := newRootCommand(&commandRuntime{})
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetArgs([]string{"--version"})
		Expect(root.Execute()).To(Succeed())
		Expect(output.String()).To(Equal("uir version dev\n"))
	})
	It("prints the root version through a local version command without opening a database", func() {
		runtime := &commandRuntime{DSN: "invalid"}
		root := newRootCommand(runtime)
		root.Version = "v1.2.3"
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetArgs([]string{"version"})
		Expect(root.Execute()).To(Succeed())
		Expect(output.String()).To(Equal("uir version v1.2.3\n"))
		Expect(runtime.database).To(BeNil())
		Expect(clicky.IsLocalOnly(findCommand(root, "version"))).To(BeTrue())
	})

	It("sets commons properties from -P on any subcommand", func() {
		const key, value = "log.level.uir-test", "trace"
		root := newRootCommand(&commandRuntime{})
		root.SetOut(&bytes.Buffer{})
		root.SetArgs([]string{"version", "-P", key + "=" + value})
		Expect(root.Execute()).To(Succeed())
		Expect(properties.Get(key)).To(Equal(value))
	})

	It("registers the module operations without a project entity", func() {
		root := newRootCommand(&commandRuntime{})
		Expect(commandNames(root)).To(ContainElements("add", "list", "get", "query", "reindex", "locations", "snapshots", "browse", "content", "serve", "version"))
		Expect(commandNames(root)).ToNot(ContainElement("project"))
	})
})

func openCommandDatabase(ctx context.Context) *gorm.DB {
	GinkgoHelper()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(GinkgoT().TempDir(), "command.db")})
	Expect(err).To(Succeed())
	DeferCleanup(func() {
		// The task-run store is process-global: a runtime that installed it over this database must not
		// leave its writer running into the next spec, which rebinds the global properties it reads.
		clickytask.SetStore(context.Background(), nil)
		sqlDB, dbErr := database.DB()
		Expect(dbErr).To(Succeed())
		Expect(sqlDB.Close()).To(Succeed())
	})
	return database
}

func findCommand(parent *cobra.Command, name string) *cobra.Command {
	GinkgoHelper()
	for _, command := range parent.Commands() {
		if command.Name() == name {
			return command
		}
	}
	Fail("command " + name + " was not generated")
	return nil
}

func commandNames(parent *cobra.Command) []string {
	names := make([]string, 0, len(parent.Commands()))
	for _, command := range parent.Commands() {
		names = append(names, command.Name())
	}
	return names
}
