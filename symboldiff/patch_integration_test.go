package symboldiff

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const patchModule = "example.org/patchdiff"

var (
	patchCalcBefore = lines(
		"package calc", "",
		"// Point is a position.", "type Point struct {", "\tX int", "\ty int", "}", "",
		"func Add(a, b int) int { return a + b }", "",
		"func Sub(a, b int) int { return a - b }", "",
		"func Scale(a int, factor int) int { return a * factor }",
	)
	patchCalcAfter = lines(
		"package calc", "",
		"// Point is a position.", "type Point struct {", "\tX int", "\ty string", "}", "",
		"func Add(a, b int) int { return b + a }", "",
		"func Scale(a int, by int) int { return a * by }", "",
		"func Mul(a, b int) int { return a * b }",
	)
	patchHelper      = lines("package calc", "", "func Helper() int { return 1 }")
	patchHelperMoved = lines("package calc", "", "// Helper moved here.", "func Helper() int { return 1 }")
)

// newPatchRepository commits a module, then edits, removes, renames, and adds files in a second commit.
func newPatchRepository() *repository {
	GinkgoHelper()
	repo := newRepository()
	repo.write(map[string]*string{
		"go.mod": text("module " + patchModule + "\n\ngo 1.26\n"), "README.md": text("hello\n"),
		"calc/calc.go": text(patchCalcBefore), "calc/old_name.go": text(patchHelper),
	})
	repo.commit("first")
	repo.write(map[string]*string{
		"README.md": text("hello world\n"), "calc/calc.go": text(patchCalcAfter),
		"calc/old_name.go": nil, "calc/new_name.go": text(patchHelperMoved),
	})
	repo.commit("second")
	return repo
}

// patchOf is the repository's git diff output, untrimmed, as a pipe would carry it.
func patchOf(repo *repository, args ...string) string {
	GinkgoHelper()
	command := exec.Command("git", append([]string{"-C", repo.path, "diff"}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.Output()
	Expect(err).To(Succeed())
	return string(output)
}

func gitLocator(ctx context.Context, repo *repository) *GitLocator {
	GinkgoHelper()
	locator, err := NewGitLocator(ctx, repo.path)
	Expect(err).To(Succeed())
	return locator
}

// recordGitCommands records the subcommand of every git process the git locator starts.
func recordGitCommands() *[]string {
	var commands []string
	original := gitCommand
	gitCommand = func(ctx context.Context, directory string, args ...string) *exec.Cmd {
		commands = append(commands, args[0])
		return original(ctx, directory, args...)
	}
	DeferCleanup(func() { gitCommand = original })
	return &commands
}

type patchRow struct {
	Path, Class, Name, Visibility, ShapeBefore, ShapeAfter, PathBefore string
	MovedAway                                                          bool
}

func patchRows(result Result) []patchRow {
	var rows []patchRow
	for _, pkg := range result.Packages {
		for _, file := range pkg.Files {
			for _, row := range file.Rows {
				Expect(row.Coverage).To(Equal([]string{MarkerSyntax}), row.DisplayName())
				rows = append(rows, patchRow{file.Path, string(row.Class), row.DisplayName(), row.Visibility, row.ShapeBefore, row.ShapeAfter, row.PathBefore, row.MovedAway})
			}
		}
	}
	return rows
}

var _ = Describe("DiffPatch", func() {
	It("classifies every symbol a committed patch changes by layer, and pairs a renamed file's symbols as moved", func(ctx SpecContext) {
		repo := newPatchRepository()
		result, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(patchOf(repo, "HEAD~1")), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll})
		Expect(err).To(Succeed())
		Expect([]string{result.RootKey, result.From.Commit, result.To.Commit}).To(Equal([]string{patchModule, PatchOld, PatchNew}))
		var files []FileDiff
		for _, pkg := range result.Packages {
			for _, file := range pkg.Files {
				files = append(files, FileDiff{Path: pkg.Path + " " + file.Path, Status: file.Status, Coverage: file.Coverage, Excluded: file.Excluded})
			}
		}
		Expect(files).To(Equal([]FileDiff{
			{Path: patchModule + " README.md", Status: FileModified, Coverage: []string{MarkerExcluded}, Excluded: NotGoSource},
			{Path: patchModule + "/calc calc/calc.go", Status: FileModified, Coverage: []string{MarkerSyntax}},
			{Path: patchModule + "/calc calc/new_name.go", Status: FileAdded, Coverage: []string{MarkerSyntax}},
			{Path: patchModule + "/calc calc/old_name.go", Status: FileRemoved, Coverage: []string{MarkerSyntax}},
		}))
		Expect(patchRows(result)).To(Equal([]patchRow{
			{Path: "calc/calc.go", Class: "body", Name: "Add", Visibility: "exported", PathBefore: "calc/calc.go"},
			{Path: "calc/calc.go", Class: "added", Name: "Mul", Visibility: "exported", ShapeAfter: "method public (a (int), b (int)) ( (int))"},
			{
				Path: "calc/calc.go", Class: "structure", Name: "Point", Visibility: "exported", PathBefore: "calc/calc.go",
				ShapeBefore: "class public {\n  record_field X (int) public\n  record_field y (int) private\n}",
				ShapeAfter:  "class public {\n  record_field X (int) public\n  record_field y (string) private\n}",
			},
			{
				Path: "calc/calc.go", Class: "signature", Name: "Point.y", Visibility: "internal", PathBefore: "calc/calc.go",
				ShapeBefore: "record_field (int) private", ShapeAfter: "record_field (string) private",
			},
			{
				Path: "calc/calc.go", Class: "signature", Name: "Scale", Visibility: "exported", PathBefore: "calc/calc.go",
				ShapeBefore: "method public (a (int), factor (int)) ( (int))", ShapeAfter: "method public (a (int), by (int)) ( (int))",
			},
			{Path: "calc/calc.go", Class: "removed", Name: "Sub", Visibility: "exported", ShapeBefore: "method public (a (int), b (int)) ( (int))", PathBefore: "calc/calc.go"},
			{Path: "calc/new_name.go", Class: "moved", Name: "Helper", Visibility: "exported", PathBefore: "calc/old_name.go"},
			{Path: "calc/old_name.go", Class: "moved", Name: "Helper", Visibility: "exported", PathBefore: "calc/old_name.go", MovedAway: true},
		}))
	})

	DescribeTable("reports only the row classes its change types match, omitting files left without a row",
		func(ctx SpecContext, changeTypes ChangeTypes, shown []string, hidden, hiddenFiles int) {
			repo := newPatchRepository()
			result, err := DiffPatch(ctx, PatchOptions{
				Patch: strings.NewReader(patchOf(repo, "HEAD~1")), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll, ChangeTypes: changeTypes,
			})
			Expect(err).To(Succeed())
			var rows []string
			hiddenRows, omitted := 0, 0
			for _, pkg := range result.Packages {
				omitted += pkg.HiddenFiles
				for _, file := range pkg.Files {
					hiddenRows += file.HiddenRows
					for _, row := range file.Rows {
						rows = append(rows, file.Path+" "+string(row.Class)+" "+row.DisplayName())
					}
				}
			}
			Expect([]any{result.ChangeTypes, rows, hiddenRows, omitted}).To(Equal([]any{changeTypes, shown, hidden, hiddenFiles}))
		},
		// The README's package has no file left, so it is omitted with its hidden file.
		Entry("moved only, on both files", ChangeTypes{"moved"},
			[]string{"calc/new_name.go moved Helper", "calc/old_name.go moved Helper"}, 0, 1),
		Entry("everything but added and removed", ChangeTypes{"!added", "!removed"},
			[]string{"calc/calc.go body Add", "calc/calc.go structure Point", "calc/calc.go signature Point.y", "calc/calc.go signature Scale",
				"calc/new_name.go moved Helper", "calc/old_name.go moved Helper"}, 2, 0),
		Entry("a wildcard", ChangeTypes{"s*"},
			[]string{"calc/calc.go structure Point", "calc/calc.go signature Point.y", "calc/calc.go signature Scale"}, 3, 2),
	)

	It("pairs a symbol moved to another package when it is the only candidate and its shape differs only by the move", func(ctx SpecContext) {
		repo := newRepository()
		repo.write(map[string]*string{
			"go.mod": text("module " + patchModule + "\n\ngo 1.26\n"),
			"app/app.go": text(lines("package app", "", `import "`+patchModule+`/models"`, "",
				"func Pure(a, b int) int { return a + b }", "", "func Retyped(item models.Item) int { return item.N }", "", "func Twin() int { return 1 }")),
			"models/models.go": text(lines("package models", "", "type Item struct{ N int }")),
		})
		repo.commit("before")
		repo.write(map[string]*string{
			"app/app.go": text(lines("package app", "", "func Keep() int { return 0 }")),
			"models/models.go": text(lines("package models", "", "type Item struct{ N int }", "",
				"func Pure(a, b int) int { return a + b }", "", "func Retyped(item Item) int { return item.N }", "", "func Twin() int { return 1 }")),
			"other/other.go": text(lines("package other", "", "func Twin() int { return 1 }")),
		})
		repo.commit("after")
		patch := patchOf(repo, "HEAD~1")

		all, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(patch), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll})
		Expect(err).To(Succeed())
		moved, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(patch), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll, ChangeTypes: ChangeTypes{"moved"}})
		Expect(err).To(Succeed())

		Expect(patchRows(all)).To(Equal([]patchRow{
			{Path: "app/app.go", Class: "added", Name: "Keep", Visibility: "exported", ShapeAfter: "method public () ( (int))"},
			{Path: "app/app.go", Class: "moved", Name: "Pure", Visibility: "exported", PathBefore: "app/app.go", MovedAway: true},
			{
				Path: "app/app.go", Class: "signature", Name: "Retyped", Visibility: "exported", PathBefore: "app/app.go", MovedAway: true,
				ShapeBefore: "method public (item (models.Item)) ( (int))", ShapeAfter: "method public (item (Item)) ( (int))",
			},
			{Path: "app/app.go", Class: "removed", Name: "Twin", Visibility: "exported", ShapeBefore: "method public () ( (int))", PathBefore: "app/app.go"},
			{Path: "models/models.go", Class: "moved", Name: "Pure", Visibility: "exported", PathBefore: "app/app.go"},
			{
				Path: "models/models.go", Class: "signature", Name: "Retyped", Visibility: "exported", PathBefore: "app/app.go",
				ShapeBefore: "method public (item (models.Item)) ( (int))", ShapeAfter: "method public (item (Item)) ( (int))",
			},
			{Path: "models/models.go", Class: "added", Name: "Twin", Visibility: "exported", ShapeAfter: "method public () ( (int))"},
			{Path: "other/other.go", Class: "added", Name: "Twin", Visibility: "exported", ShapeAfter: "method public () ( (int))"},
		}))
		Expect(patchRows(moved)).To(Equal([]patchRow{
			{Path: "app/app.go", Class: "moved", Name: "Pure", Visibility: "exported", PathBefore: "app/app.go", MovedAway: true},
			{Path: "models/models.go", Class: "moved", Name: "Pure", Visibility: "exported", PathBefore: "app/app.go"},
		}))
	})

	It("refuses a change type that names no class", func(ctx SpecContext) {
		repo := newPatchRepository()
		_, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(patchOf(repo, "HEAD~1")), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll, ChangeTypes: ChangeTypes{"!removd"}})
		Expect(err).To(MatchError(`change type "!removd" matches none of added, removed, signature, structure, body, moved`))
	})

	It("reads a worktree diff's new side from the worktree and counts lines as git does", func(ctx SpecContext) {
		repo := newPatchRepository()
		repo.write(map[string]*string{"calc/calc.go": text(patchCalcAfter + "\nfunc Div(a, b int) int { return a / b }\n")})
		result, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(patchOf(repo, "HEAD")), Locator: gitLocator(ctx, repo), Visibility: VisibilityExported, Stat: true})
		Expect(err).To(Succeed())
		Expect(result.To.Commit).To(Equal(PatchWorktree))
		file := result.Packages[0].Files[0]
		Expect([]any{file.Path, file.Rows[0].Class, file.Rows[0].Name, *file.Lines}).To(Equal([]any{"calc/calc.go", ClassAdded, "Div", LineCount{Added: 2}}))
	})

	DescribeTable("pairs a symbol moved into an untracked file only when the worktree patch includes untracked files, leaving the index unchanged",
		func(ctx SpecContext, untracked bool, expected []patchRow) {
			repo := newPatchRepository()
			repo.write(map[string]*string{"calc/new_name.go": nil, "calc/untracked.go": text(patchHelperMoved)})
			index := filepath.Join(repo.path, ".git", "index")
			before, err := os.ReadFile(index)
			Expect(err).To(Succeed())
			patch, err := WorktreePatch(ctx, WorktreePatchOptions{Checkout: repo.path, Commit: repo.git("rev-parse", "HEAD"), Untracked: untracked})
			Expect(err).To(Succeed())
			result, err := DiffPatch(ctx, PatchOptions{Patch: patch, Locator: gitLocator(ctx, repo), Visibility: VisibilityAll})
			Expect(err).To(Succeed())
			after, err := os.ReadFile(index)
			Expect(err).To(Succeed())
			Expect([]any{patchRows(result), after}).To(Equal([]any{expected, before}))
		},
		Entry("tracked files only, as git diff", false, []patchRow{
			{Path: "calc/new_name.go", Class: "removed", Name: "Helper", Visibility: "exported", ShapeBefore: "method public () ( (int))", PathBefore: "calc/new_name.go"},
		}),
		Entry("with untracked files", true, []patchRow{
			{Path: "calc/new_name.go", Class: "moved", Name: "Helper", Visibility: "exported", PathBefore: "calc/new_name.go", MovedAway: true},
			{Path: "calc/untracked.go", Class: "moved", Name: "Helper", Visibility: "exported", PathBefore: "calc/new_name.go"},
		}),
	)

	It("refuses a worktree diff whose file changed after the patch was produced", func(ctx SpecContext) {
		repo := newPatchRepository()
		repo.write(map[string]*string{"calc/calc.go": text(patchCalcAfter + "\nfunc Div(a, b int) int { return a / b }\n")})
		patch := patchOf(repo, "HEAD")
		repo.write(map[string]*string{"calc/calc.go": text(patchCalcAfter)})
		_, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(patch), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll})
		Expect(err).To(MatchError(ContainSubstring("worktree changed since the patch was produced: calc/calc.go hashes to")))
	})

	It("counts the lines of a file that is not renamed as git's numstat does", func(ctx SpecContext) {
		repo := newPatchRepository()
		result, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(patchOf(repo, "HEAD~1")), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll, Stat: true})
		Expect(err).To(Succeed())
		counts := map[string]*LineCount{}
		for _, pkg := range result.Packages {
			for _, file := range pkg.Files {
				counts[file.Path] = file.Lines
			}
		}
		for _, path := range []string{"README.md", "calc/calc.go"} {
			Expect(counts[path]).To(Equal(new(repo.numstat("HEAD~1", "HEAD", path))), path)
		}
	})

	DescribeTable("reads every side of a patch with one cat-file and one hash-object process, whatever the file count",
		func(ctx SpecContext, fileCount int) {
			repo := newRepository()
			for revision := range 3 {
				files := map[string]*string{"go.mod": text("module " + patchModule + "\n\ngo 1.26\n")}
				for index := range fileCount {
					files[fmt.Sprintf("calc/f%d.go", index)] = text(fmt.Sprintf("package calc\n\nfunc F%d() int { return %d }\n", index, revision))
				}
				repo.write(files)
				if revision < 2 {
					repo.commit(fmt.Sprintf("revision %d", revision))
				}
			}
			commands := recordGitCommands()
			result, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(patchOf(repo, "HEAD~1")), Locator: gitLocator(ctx, repo), Visibility: VisibilityAll})
			Expect(err).To(Succeed())
			Expect([]any{result.To.Commit, len(result.Packages[0].Files), *commands}).To(Equal([]any{PatchWorktree, fileCount, []string{"rev-parse", "cat-file", "hash-object"}}))
		},
		Entry("one file", 1),
		Entry("twelve files", 12),
	)
})

var _ = Describe("GitLocator", func() {
	It("resolves an abbreviated and a full blob id to the same bytes", func(ctx SpecContext) {
		repo := newPatchRepository()
		full := repo.git("rev-parse", "HEAD:calc/calc.go")
		blobs, err := gitLocator(ctx, repo).Read(ctx, []BlobRequest{{Path: "calc/calc.go", ID: full[:7]}, {Path: "calc/calc.go", ID: full, New: true}})
		Expect(err).To(Succeed())
		Expect(blobs).To(Equal([]Blob{{Content: []byte(patchCalcAfter)}, {Content: []byte(patchCalcAfter)}}))
	})

	It("refuses an old side whose blob the repository does not have, naming its path and id", func(ctx SpecContext) {
		repo := newPatchRepository()
		_, err := gitLocator(ctx, repo).Read(ctx, []BlobRequest{{Path: "calc/gone.go", ID: "badc0de"}})
		Expect(err).To(MatchError("old side of calc/gone.go: blob badc0de is missing from the repository"))
	})
})
