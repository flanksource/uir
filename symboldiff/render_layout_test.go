package symboldiff

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("diff terminal layout", func() {
	It("renders package, file, symbol, and shape branches with change icons and body counts", func() {
		result := Result{RootKey: "example.org/ledger", From: Side{Commit: "old"}, To: Side{Commit: "new"}, Stat: true, Packages: []PackageDiff{{
			Path: "example.org/ledger/core", Lines: &LineCount{Added: 3, Removed: 2}, Files: []FileDiff{{
				Path: "core/amount.go", Status: FileModified, Lines: &LineCount{Added: 3, Removed: 2}, FileScope: &LineCount{}, Rows: []Row{{
					Class: ClassSignature, Kind: "func", Name: "Scale", Lines: &LineCount{Added: 1, Removed: 1},
					ShapeDiff: DiffShapes("func Scale(a int) int", "func Scale(a Money) int", TokenizerGo),
				}, {
					Class: ClassBody, Kind: "func", Name: "Compute", Lines: &LineCount{Added: 2, Removed: 1},
				}},
			}},
		}, {
			Path: "example.org/ledger/tools", Lines: &LineCount{Added: 1}, Files: []FileDiff{{
				Path: "tools/log.go", Status: FileAdded, Lines: &LineCount{Added: 1}, Rows: []Row{{
					Class: ClassAdded, Kind: "func", Name: "Log", Lines: &LineCount{Added: 1},
				}},
			}},
		}}}

		plain := result.Pretty().String()
		Expect(strings.Split(strings.TrimSuffix(plain, "\n"), "\n")).To(Equal([]string{
			"example.org/ledger old..new",
			"├── 📦 example.org/ledger/core  +3 -2",
			"│   ╰── ✏️ core/amount.go (modified)  +3 -2",
			"│       ├── ✏️ Scale  signature  +1 -1",
			"│       │   ╰── - func Scale(a int) int",
			"│       │       + func Scale(a Money) int",
			"│       ├── 💻 Compute  body  +2 -1",
			"│       ╰── (file scope)  +0 -0",
			"╰── 📦 example.org/ledger/tools  +1 -0",
			"    ╰── ➕ tools/log.go (added)  +1 -0",
			"        ╰── ➕ Log  added  +1 -0",
		}))
		ansi := result.Pretty().ANSI()
		Expect(ansi).To(And(ContainSubstring("├──"), ContainSubstring("💻"), MatchRegexp(`\x1b\[[0-9;]*m\+2`), MatchRegexp(`\x1b\[[0-9;]*m-1`), MatchRegexp(`\x1b\[9m\x1b\[[0-9;]*mint`)))
	})

	It("uses a distinct icon for each symbol change class", func() {
		for _, example := range []struct {
			class Class
			icon  string
		}{
			{ClassAdded, "➕"}, {ClassRemoved, "➖"}, {ClassSignature, "✏️"},
			{ClassStructure, "🔨"}, {ClassBody, "💻"}, {ClassMoved, "🔀"},
		} {
			row := Row{Class: example.class, Name: "Amount"}
			want := example.icon + " Amount  " + string(example.class)
			if example.class == ClassMoved {
				row.PathBefore, row.PathAfter = "before.go", "after.go"
				want += "  from before.go"
			}
			Expect(row.tree(false).String()).To(Equal(want))
		}
	})
})
