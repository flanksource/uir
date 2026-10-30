package lint

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Lint configuration fragments", func() {
	var home, root string
	write := func(path, content string) {
		GinkgoHelper()
		Expect(os.MkdirAll(filepath.Dir(path), 0o700)).To(Succeed())
		Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
	}
	BeforeEach(func() {
		home, root = GinkgoT().TempDir(), GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", home)
		write(filepath.Join(root, ".git"), "gitdir: elsewhere")
	})

	It("merges home and project fragments in filename order without rooting inside .uir.d", func() {
		write(filepath.Join(home, ".uir.d", "base.yaml"), strings.ReplaceAll(validConfig, "id: small", "id: home"))
		write(filepath.Join(root, ConfigName), validConfig)
		write(filepath.Join(root, ".uir.d", "20-last.yml"), strings.ReplaceAll(validConfig, "is small", "last"))
		write(filepath.Join(root, ".uir.d", "10-first.yaml"), strings.ReplaceAll(validConfig, "is small", "first"))
		write(filepath.Join(root, ".uir.d", "partial.tmp"), "invalid: [")
		config, err := Discover(DiscoveryOptions{Directory: root})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Root).To(Equal(root))
		Expect(config.Lint.Rules).To(HaveExactElements(
			HaveField("ID", "home"),
			And(HaveField("ID", "small"), HaveField("Message", "{{.path}} last")),
		))
	})

	It("deletes expired fragments before validating their rules and retains future fragments", func() {
		expired := filepath.Join(root, ".uir.d", "expired.yaml")
		future := filepath.Join(root, ".uir.d", "future.yaml")
		write(expired, "_expires: 2000-01-01T00:00:00Z\nversion: 99\nlint: invalid\n")
		write(future, "_expires: 2999-01-01T00:00:00Z\n"+validConfig)
		config, err := Discover(DiscoveryOptions{Directory: root})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Root).To(Equal(root))
		Expect(config.Lint.Rules).To(HaveLen(1))
		Expect(expired).NotTo(BeAnExistingFile())
		Expect(future).To(BeAnExistingFile())
	})

	It("reports invalid expiry timestamps with the fragment path", func() {
		path := filepath.Join(root, ".uir.d", "invalid.yaml")
		write(path, "_expires: tomorrow\n"+validConfig)
		_, err := Discover(DiscoveryOptions{Directory: root})
		Expect(err).To(MatchError(And(ContainSubstring(path), ContainSubstring("_expires"))))
		Expect(path).To(BeAnExistingFile())
	})

	It("does not load home fragments inside a captured source boundary", func() {
		write(filepath.Join(home, ".uir.d", "invalid.yaml"), "invalid: [")
		write(filepath.Join(root, ".uir.d", "project.yaml"), validConfig)
		config, err := Discover(DiscoveryOptions{Directory: root, Boundary: root})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Lint.Rules).To(HaveLen(1))
	})

	It("uses the Git root with only home fragments", func() {
		write(filepath.Join(home, ".uir.d", "base.yaml"), validConfig)
		nested := filepath.Join(root, "nested")
		Expect(os.Mkdir(nested, 0o700)).To(Succeed())
		config, err := Discover(DiscoveryOptions{Directory: nested})
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Root).To(Equal(root))
		Expect(config.Lint.Rules).To(HaveLen(1))
	})

	It("fails loudly when an expired fragment cannot be deleted", func() {
		path := filepath.Join(root, ".uir.d", "expired.yaml")
		write(path, "_expires: 2000-01-01T00:00:00Z\n"+validConfig)
		Expect(os.Chmod(filepath.Dir(path), 0o500)).To(Succeed())
		DeferCleanup(os.Chmod, filepath.Dir(path), os.FileMode(0o700))
		_, err := Discover(DiscoveryOptions{Directory: root})
		Expect(err).To(MatchError(And(ContainSubstring(path), ContainSubstring("delete expired"))))
		Expect(path).To(BeAnExistingFile())
	})

	It("reports no configuration when every discovered fragment has expired", func() {
		path := filepath.Join(root, ".uir.d", "expired.yaml")
		write(path, "_expires: 2000-01-01T00:00:00Z\n"+validConfig)
		_, err := Discover(DiscoveryOptions{Directory: root})
		Expect(err).To(MatchError(ContainSubstring("no unexpired lint configuration")))
		Expect(path).NotTo(BeAnExistingFile())
	})
})
