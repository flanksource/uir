package query

import (
	"github.com/flanksource/uir/storage/symbolhandle"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("selector handle ranges", func() {
	// The module field starts at bit 52 and the package field at bit 36 of an H64b handle.
	packageRange := func(module, pkg int64) symbolhandle.Range {
		low := module<<52 | pkg<<36
		return symbolhandle.Range{Low: low, High: low + 1<<36 - 1}
	}

	It("spans every package of a module and nothing of the next", func() {
		Expect(moduleRange(3)).To(Equal(symbolhandle.Range{Low: 3 << 52, High: 4<<52 - 1}))
		_, err := moduleRange(symbolhandle.MaxModule + 1)
		Expect(err).To(MatchError(ContainSubstring("module")))
	})

	It("merges consecutive and repeated package ranges and keeps gaps", func() {
		merged := mergeRanges([]symbolhandle.Range{
			packageRange(3, 0), packageRange(2, 5), packageRange(2, 1), packageRange(2, 0), packageRange(2, 1), packageRange(2, 2),
		})
		Expect(merged).To(Equal([]symbolhandle.Range{
			{Low: packageRange(2, 0).Low, High: packageRange(2, 2).High}, packageRange(2, 5), packageRange(3, 0),
		}))
		Expect(mergeRanges(nil)).To(BeEmpty())
	})

	It("admits the package of every qualified name a pattern matches and rejects unrelated packages", func() {
		packages := []string{"example.org/app", "example.org/app/rpc", "example.org/app/rpc/deep", "example.org/apple", "gopkg.in/yaml.v3", "fmt"}
		names := []string{"New", "NewServer", "Server.Start", "v3.Marshal"}
		patterns := []string{
			"example.org/app/**", "example.org/app/rpc.New*", "example.org/app/**/rpc.Server.Start", "example.org/app.New?",
			"gopkg.in/yaml.v3.*", "**/deep.New", "example.org/app/rpc.Server\\*", "*/app.New",
		}
		admitted := 0
		for _, pattern := range patterns {
			glob, err := compileSelectorGlob(pattern)
			Expect(err).ToNot(HaveOccurred())
			keep := qualifiedPackageFilter(pattern)
			for _, pkg := range packages {
				for _, name := range names {
					if glob.matches(pkg + "." + name) {
						admitted++
						Expect(keep(pkg, "")).To(BeTrue(), "%s matches %s.%s", pattern, pkg, name)
					}
				}
			}
		}
		Expect(admitted).To(BeNumerically(">=", 6), "the patterns match enough keys to exercise the filter")
		Expect(qualifiedPackageFilter("example.org/app/rpc.New*")("example.org/apple", "")).To(BeFalse())
		Expect(qualifiedPackageFilter("example.org/app/**")("example.org/app", "")).To(BeFalse(), "app/** matches below app only")
		Expect(qualifiedPackageFilter("example.org/app/**")("fmt", "")).To(BeFalse())
	})

	It("names a package relative to its module root", func() {
		for _, entry := range []struct {
			path, relative string
			inside         bool
		}{
			{"example.org/shop", ".", true},
			{"example.org/shop/app/sub", "app/sub", true},
			{"example.org/shopping/app", "", false},
			{"example.org/other", "", false},
		} {
			relative, inside := relativePackage(entry.path, "example.org/shop")
			Expect([]any{relative, inside}).To(Equal([]any{entry.relative, entry.inside}), entry.path)
		}
	})
})
