package main

import (
	"encoding/json"
	"slices"

	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("commands over every root", func() {
	type scopeCase struct {
		inCheckout bool
		args       []string
		roots      []string
	}
	everyRoot := []string{diffRoot, referencesRoot}

	DescribeTable("scope query and prune to the working directory's checkout unless --all",
		func(ctx SpecContext, scenario scopeCase) {
			database := openCommandDatabase(ctx)
			_, _, workspace := indexedCommits(ctx, database)
			_, err := addModules(ctx, database, writeReferencesModule(), false)
			Expect(err).ToNot(HaveOccurred())
			runtime := &commandRuntime{database: database}
			chdir(GinkgoT().TempDir())
			if scenario.inCheckout {
				chdir(workspace)
			}

			output, err := runCLI(ctx, runtime, append(scenario.args, "--format", "json")...)
			Expect(err).ToNot(HaveOccurred(), output)
			Expect(reportedRoots(scenario.args[0], output)).To(Equal(scenario.roots))
		},
		Entry("query inside a checkout", scopeCase{inCheckout: true, args: []string{"query", "--packages"}, roots: []string{diffRoot}}),
		Entry("query inside a checkout with --all", scopeCase{inCheckout: true, args: []string{"query", "--packages", "--all"}, roots: everyRoot}),
		Entry("query outside any checkout", scopeCase{args: []string{"query", "--packages"}, roots: everyRoot}),
		Entry("prune inside a checkout", scopeCase{inCheckout: true, args: []string{"prune", "--keep", "5"}, roots: []string{diffRoot}}),
		Entry("prune inside a checkout with --all", scopeCase{inCheckout: true, args: []string{"prune", "--keep", "5", "--all"}, roots: everyRoot}),
	)

	DescribeTable("refuse --all with an explicit root or location",
		func(ctx SpecContext, args []string) {
			runtime := &commandRuntime{database: openCommandDatabase(ctx)}
			_, err := runCLI(ctx, runtime, args...)
			Expect(err).To(MatchError(ContainSubstring("--all cannot be combined with --root, --location, or --snapshot")))
		},
		Entry("query", []string{"query", "--packages", "--all", "--root", diffRoot}),
		Entry("suggest", []string{"suggest", "--prefix", "Run", "--all", "--location", "."}),
		Entry("graph", []string{"graph", "Run", "--all", "--root", diffRoot}),
		Entry("browse", []string{"browse", "--all", "--root", diffRoot}),
		Entry("prune", []string{"prune", diffRoot, "--keep", "1", "--all"}),
	)
})

// reportedRoots is the sorted, distinct roots a query's matches or a prune's results name.
func reportedRoots(command, output string) []string {
	GinkgoHelper()
	var roots []string
	switch command {
	case "query":
		var matches []moduleQueryRow
		Expect(json.Unmarshal([]byte(output), &matches)).To(Succeed(), output)
		for _, match := range matches {
			roots = append(roots, match.Root)
		}
	case "prune":
		var results []storage.PruneResult
		Expect(json.Unmarshal([]byte(output), &results)).To(Succeed(), output)
		for _, result := range results {
			roots = append(roots, result.RootKey)
		}
	default:
		Fail("no reported roots for command " + command)
	}
	slices.Sort(roots)
	return slices.Compact(roots)
}
