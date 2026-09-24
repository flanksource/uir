package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/flanksource/clicky"
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
		runtime := &commandRuntime{}
		_, err := runtime.Database(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { Expect(runtime.Close()).To(Succeed()) })
		Expect(runtime.DSN).To(Equal(filepath.Join(home, ".config", "uir", "uir.db")))
		_, err = os.Stat(runtime.DSN)
		Expect(err).ToNot(HaveOccurred())
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
