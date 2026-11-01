package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLint(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "UIR lint") }

const validConfig = "version: 1\nlint:\n  rules:\n    - id: small\n      query: SELECT path FROM files()\n      message: '{{.path}} is small'\n"

func configFile(content string) string {
	GinkgoHelper()
	path := filepath.Join(GinkgoT().TempDir(), ConfigName)
	Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
	return path
}

var _ = Describe("Lint configuration", func() {
	It("loads defaults and selects exact IDs while rejecting unknown IDs", func() {
		config, err := Load(configFile(validConfig))
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Lint.Rules[0].Severity).To(BeEquivalentTo("error"))
		rules, err := config.Select([]string{"small"})
		Expect(err).NotTo(HaveOccurred())
		Expect(rules).To(HaveLen(1))
		_, err = config.Select([]string{"typo"})
		Expect(err).To(MatchError(ContainSubstring("typo")))
	})
	DescribeTable("rejects invalid configuration", func(content string) {
		_, err := Load(configFile(content))
		Expect(err).To(HaveOccurred())
	},
		Entry("version", "version: 2\nlint: {rules: []}"),
		Entry("empty rules", "version: 1\nlint: {rules: []}"),
		Entry("duplicate keys", validConfig+"version: 1\n"),
		Entry("unknown field", validConfig+"unknown: true\n"),
		Entry("unknown nested field", validConfig+"  typo: true\n"),
		Entry("multiple documents", validConfig+"---\n"+validConfig),
		Entry("severity", validConfig+"      severity: fatal\n"),
		Entry("pagination", strings.Replace(validConfig, "SELECT path FROM files()", "SELECT path FROM files() LIMIT 2", 1)),
		Entry("template", strings.Replace(validConfig, "{{.path}} is small", "{{", 1)),
		Entry("empty id", strings.Replace(validConfig, "id: small", "id: ''", 1)),
		Entry("missing query", strings.Replace(validConfig, "query: SELECT path FROM files()", "query: ''", 1)),
		Entry("missing message", strings.Replace(validConfig, "{{.path}} is small", "", 1)),
		Entry("duplicate ids", validConfig+"    - {id: small, query: 'SELECT path FROM files()', message: duplicate}\n"),
		Entry("reserved rule", "version: 1\nlint:\n  rules:\n    - {id: uir/coverage, message: coverage, query: 'SELECT path FROM files()'}\n"),
	)
})
