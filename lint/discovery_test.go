package lint

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Hierarchical lint configuration", func() {
	var home, parent, root, nested string
	write := func(directory, content string) {
		GinkgoHelper()
		Expect(os.WriteFile(filepath.Join(directory, ConfigName), []byte(content), 0o600)).To(Succeed())
	}
	BeforeEach(func() {
		home = GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		parent = GinkgoT().TempDir()
		root = filepath.Join(parent, "project")
		nested = filepath.Join(root, "nested")
		Expect(os.MkdirAll(nested, 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: elsewhere"), 0o600)).To(Succeed())
	})

	It("merges home and ancestors across Git roots with closer rules replacing whole definitions", func() {
		write(home, validConfig+"    - {id: home, query: 'SELECT path FROM files()', message: home}\n")
		write(parent, validConfig+"    - {id: parent, query: 'SELECT path FROM files()', message: parent}\n")
		write(root, strings.Replace(validConfig, "is small", "from root", 1)+"      severity: warning\n")
		write(nested, strings.Replace(validConfig, "is small", "from nested", 1))
		config, err := Discover(DiscoveryOptions{Directory: nested, Explicit: ""})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Root).To(Equal(nested))
		Expect(config.Path).To(Equal(filepath.Join(nested, ConfigName)))
		Expect(config.Lint.Rules).To(HaveExactElements(
			And(HaveField("ID", "small"), HaveField("Message", "{{.path}} from nested"), HaveField("Severity", BeEquivalentTo("error"))),
			HaveField("ID", "home"), HaveField("ID", "parent"),
		))
	})

	It("inherits include_tests when omitted and lets explicit false override true", func() {
		write(home, strings.Replace(validConfig, "lint:\n", "lint:\n  include_tests: true\n", 1))
		write(root, validConfig)
		config, err := Discover(DiscoveryOptions{Directory: nested, Explicit: ""})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Lint.IncludeTests).To(BeTrue())
		write(nested, strings.Replace(validConfig, "lint:\n", "lint:\n  include_tests: false\n", 1))
		config, err = Discover(DiscoveryOptions{Directory: nested, Explicit: ""})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Lint.IncludeTests).To(BeFalse())
	})

	It("uses the Git root for home-only or external ancestor configuration", func() {
		for _, directory := range []string{home, parent} {
			write(directory, validConfig)
			config, err := Discover(DiscoveryOptions{Directory: nested, Explicit: ""})
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Root).To(Equal(root))
		}
	})

	It("uses the invocation directory with only a home config outside Git", func() {
		write(home, validConfig)
		config, err := Discover(DiscoveryOptions{Directory: parent, Explicit: ""})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Root).To(Equal(parent))
	})

	It("loads home once as the base when it is also an ancestor", func() {
		GinkgoT().Setenv("HOME", parent)
		write(parent, validConfig+"    - {id: home, query: 'SELECT path FROM files()', message: home}\n")
		write(root, strings.Replace(validConfig, "is small", "from project", 1))
		config, err := Discover(DiscoveryOptions{Directory: nested})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Root).To(Equal(root))
		Expect(config.Lint.Rules).To(HaveExactElements(
			And(HaveField("ID", "small"), HaveField("Message", "{{.path}} from project")),
			HaveField("ID", "home"),
		))
	})

	It("loads an explicit file without inheriting invalid home or parent configurations", func() {
		write(home, "invalid: [")
		write(parent, "version: 99\n")
		write(root, validConfig)
		config, err := Discover(DiscoveryOptions{Directory: nested, Explicit: filepath.Join(root, ConfigName)})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Root).To(Equal(root))
		Expect(config.Lint.Rules).To(HaveLen(1))
	})

	It("fails with the invalid inherited configuration path even if a child replaces its rule", func() {
		write(parent, strings.Replace(validConfig, "SELECT path FROM files()", "invalid SQL", 1))
		write(root, validConfig)
		_, err := Discover(DiscoveryOptions{Directory: nested, Explicit: ""})
		Expect(err).To(MatchError(ContainSubstring(filepath.Join(parent, ConfigName))))
	})

	It("fails when no configuration exists", func() {
		_, err := Discover(DiscoveryOptions{Directory: nested, Explicit: ""})
		Expect(err).To(MatchError(ContainSubstring("no .uir.yaml")))
	})

	It("does not ignore a directory named .uir.yaml", func() {
		Expect(os.Mkdir(filepath.Join(root, ConfigName), 0o700)).To(Succeed())
		_, err := Discover(DiscoveryOptions{Directory: nested, Explicit: ""})
		Expect(err).To(HaveOccurred())
	})
})
