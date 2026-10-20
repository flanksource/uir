//go:build ruleguardparity

package lint

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Ruleguard parity", func() {
	It("preserves every reference finding and additionally catches grouped global declarations", func(ctx SpecContext) {
		root := GinkgoT().TempDir()
		Expect(os.CopyFS(root, os.DirFS("testdata/request-state"))).To(Succeed())
		command := exec.CommandContext(ctx, "go", "run", "github.com/quasilyte/go-ruleguard/cmd/ruleguard@v0.4.5", "-rules", "reference.rules.go", "./...")
		command.Dir = root
		command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=go1.22.12")
		output, err := command.CombinedOutput()
		Expect(err).To(HaveOccurred(), string(output))
		findings := regexp.MustCompile(`state.go:(\d+):(\d+): noGlobalRequestState: (package-level request state [^\n]+) \(reference\.rules\.go:\d+\)`)
		matches := findings.FindAllStringSubmatch(string(output), -1)
		Expect(matches).To(HaveLen(11), string(output))
		config, err := Load(filepath.Join(root, ConfigName))
		Expect(err).NotTo(HaveOccurred())
		report := Run(ctx, lintDatabase(ctx, "sqlite"), config, Options{})
		Expect(report.Success).To(BeTrue(), report.Error)
		Expect(report.Violations).To(HaveLen(13))
		for _, match := range matches {
			line, err := strconv.Atoi(match[1])
			Expect(err).NotTo(HaveOccurred())
			column, err := strconv.Atoi(match[2])
			Expect(err).NotTo(HaveOccurred())
			Expect(report.Violations).To(ContainElement(And(HaveField("Line", line), HaveField("Column", column), HaveField("Message", HaveValue(Equal(match[3]))))))
		}
	})
})
