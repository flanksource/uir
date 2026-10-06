package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/pprof/profile"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const profileHelperEnv = "UIR_PROFILE_TEST_HELPER"

// TestUIRProfileHelper runs uir with the arguments after "--" in a child process, because run ends
// with Clicky's shutdown, which stops the task manager every other spec in this process shares.
func TestUIRProfileHelper(t *testing.T) {
	if os.Getenv(profileHelperEnv) == "" {
		return
	}
	os.Args = append([]string{"uir"}, flag.Args()...)
	main()
}

func runProfileHelper(args ...string) (string, error) {
	GinkgoHelper()
	executable, err := os.Executable()
	Expect(err).To(Succeed())
	command := exec.Command(executable, append([]string{"-test.run=^TestUIRProfileHelper$", "--"}, args...)...)
	command.Env = append(os.Environ(), profileHelperEnv+"=1")
	output, err := command.CombinedOutput()
	return string(output), err
}

func parseProfile(path string) *profile.Profile {
	GinkgoHelper()
	file, err := os.Open(path)
	Expect(err).To(Succeed())
	DeferCleanup(file.Close)
	parsed, err := profile.Parse(file)
	Expect(err).To(Succeed(), "parse profile %s", path)
	return parsed
}

func sampleTypes(parsed *profile.Profile) []string {
	types := make([]string, 0, len(parsed.SampleType))
	for _, sampleType := range parsed.SampleType {
		types = append(types, sampleType.Type)
	}
	return types
}

var _ = Describe("UIR command profiling", func() {
	It("writes a CPU profile, a heap profile and an execution trace of reindex --all", func() {
		directory := GinkgoT().TempDir()
		cpu, heap, executionTrace := filepath.Join(directory, "cpu.pprof"), filepath.Join(directory, "heap.pprof"), filepath.Join(directory, "run.trace")
		output, err := runProfileHelper("--dsn", filepath.Join(directory, "uir.db"), "reindex", "--all",
			"--cpuprofile", cpu, "--memprofile", heap, "--trace", executionTrace)
		Expect(err).To(Succeed(), output)
		Expect(sampleTypes(parseProfile(cpu))).To(Equal([]string{"samples", "cpu"}))
		Expect(sampleTypes(parseProfile(heap))).To(Equal([]string{"alloc_objects", "alloc_space", "inuse_objects", "inuse_space"}))
		info, err := os.Stat(executionTrace)
		Expect(err).To(Succeed())
		Expect(info.Size()).To(BeNumerically(">", 0))
	})

	DescribeTable("fails naming the profile file it cannot create", func(flagName string) {
		path := filepath.Join(GinkgoT().TempDir(), "missing", "profile.out")
		runtime := &commandRuntime{DSN: "invalid"}
		DeferCleanup(func() { Expect(runtime.stopProfiles()).To(Succeed()) })
		root := newRootCommand(runtime)
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs([]string{"version", "--" + flagName, path})
		Expect(root.Execute()).To(MatchError(ContainSubstring(path)))
		Expect(path).ToNot(BeAnExistingFile())
	}, Entry("CPU profile", "cpuprofile"), Entry("heap profile", "memprofile"), Entry("execution trace", "trace"))
})
