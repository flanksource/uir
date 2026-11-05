package producer_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/flanksource/uir/producer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProducer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Producer Suite")
}

var (
	stubBinary  string
	fixturePath = filepath.Join("..", "docs", "examples", "import", "co-prod.json")
)

var _ = BeforeSuite(func() {
	directory, err := os.MkdirTemp("", "stubplugin")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(os.RemoveAll, directory)
	stubBinary = filepath.Join(directory, "stub")
	output, err := exec.Command("go", "build", "-o", stubBinary, "./testdata/stubplugin").CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(output))
})

func runStub(ctx context.Context, mode string) error {
	GinkgoT().Setenv("STUB_MODE", mode)
	GinkgoT().Setenv("STUB_PUBLICATION", fixturePath)
	_, err := producer.Run(ctx, producer.Binary{Name: "stub", Path: stubBinary}, producer.PublicationRequest{Path: "."})
	return err
}

var _ = Describe("producer client", func() {
	It("returns the publication, strictly decoded, with a fingerprint of name, version and binary hash", func(ctx SpecContext) {
		GinkgoT().Setenv("STUB_PUBLICATION", fixturePath)
		publication, err := producer.Run(ctx, producer.Binary{Name: "stub", Path: stubBinary}, producer.PublicationRequest{Path: "."})
		Expect(err).ToNot(HaveOccurred())
		Expect(publication.RootKey).To(Equal("Co/Prod"))
		Expect(publication.Options.Producer).To(MatchRegexp(`^stub/9\.9\.9\+sha256:[0-9a-f]{64}$`))
	})

	DescribeTable("fails fast with the producer's own diagnosis", func(ctx SpecContext, mode string, expected ...string) {
		err := runStub(ctx, mode)
		Expect(err).To(HaveOccurred())
		for _, fragment := range expected {
			Expect(err.Error()).To(ContainSubstring(fragment))
		}
	},
		Entry("a producer on another protocol version", "wrong-protocol", `protocol "uir-producer/0"`, "upgrade"),
		Entry("a JSON-RPC error", "rpc-error", "security scan failed", "set GITHUB_TOKEN", "GITHUB_TOKEN is not set"),
		Entry("a child that exits mid-request", "crash", "exited before answering publication", "boom while scanning"),
		Entry("non-JSON on stdout", "garbage", "non-JSON on stdout"),
	)

	It("kills the child when the context is cancelled", func() {
		script := filepath.Join(GinkgoT().TempDir(), "hang")
		Expect(os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755)).To(Succeed())
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		started := time.Now()
		_, err := producer.Connect(ctx, producer.Binary{Name: "hang", Path: script})
		Expect(err).To(HaveOccurred())
		Expect(time.Since(started)).To(BeNumerically("<", 5*time.Second))
	})
})

var _ = Describe("producer discovery", func() {
	executable := func(directory string) string {
		path := filepath.Join(directory, "repomap")
		Expect(os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755)).To(Succeed())
		return path
	}

	It("prefers the configured path, then UIR_PRODUCER_PATH, then PATH", func() {
		if runtime.GOOS == "windows" {
			Skip("unix executable bits")
		}
		configured, viaEnv, viaPath := executable(GinkgoT().TempDir()), executable(GinkgoT().TempDir()), executable(GinkgoT().TempDir())
		GinkgoT().Setenv(producer.PathEnv, filepath.Dir(viaEnv))
		GinkgoT().Setenv("PATH", filepath.Dir(viaPath))

		found, err := producer.Discover("repomap", configured)
		Expect(err).ToNot(HaveOccurred())
		Expect(found.Path).To(Equal(configured))
		found, err = producer.Discover("repomap", "")
		Expect(err).ToNot(HaveOccurred())
		Expect(found.Path).To(Equal(viaEnv))
		GinkgoT().Setenv(producer.PathEnv, "")
		found, err = producer.Discover("repomap", "")
		Expect(err).ToNot(HaveOccurred())
		Expect(found.Path).To(Equal(viaPath))
	})

	It("names every searched location when the producer is missing", func() {
		empty := GinkgoT().TempDir()
		GinkgoT().Setenv(producer.PathEnv, empty)
		GinkgoT().Setenv("PATH", empty+string(os.PathListSeparator)+filepath.Join(empty, "other"))
		_, err := producer.Discover("no-such-producer", "")
		Expect(err).To(HaveOccurred())
		Expect(strings.Count(err.Error(), empty)).To(BeNumerically(">=", 2))
		Expect(err.Error()).To(And(ContainSubstring(".bin"), ContainSubstring(producer.PathEnv)))
	})

	It("rejects a configured path that is not an executable", func() {
		_, err := producer.Discover("repomap", filepath.Join(GinkgoT().TempDir(), "missing"))
		Expect(err).To(MatchError(ContainSubstring("not an executable file")))
	})
})
