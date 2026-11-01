package symboldiff

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// memoryLocator serves a patch's sides from blobs keyed by id, without git or a filesystem.
type memoryLocator struct {
	blobs   map[string][]byte
	readErr error
}

func (locator memoryLocator) Read(_ context.Context, requests []BlobRequest) ([]Blob, error) {
	if locator.readErr != nil {
		return nil, locator.readErr
	}
	blobs := make([]Blob, len(requests))
	for index, request := range requests {
		content, found := locator.blobs[request.ID]
		if !found {
			return nil, fmt.Errorf("no blob %s for %s", request.ID, request.Path)
		}
		blobs[index] = Blob{Content: content}
	}
	return blobs, nil
}

func (memoryLocator) PackagePath(file string) (string, error) {
	return path.Join(patchModule, path.Dir(file)), nil
}

func (memoryLocator) RootKey() (string, error) { return patchModule, nil }

const (
	calcBeforeBlob = "a1b2c3d"
	calcAfterBlob  = "e4f5a6b"
)

// modifiedCalcPatch is a git patch section modifying calc/calc.go; DiffPatch reads only its headers.
var modifiedCalcPatch = lines(
	"diff --git a/calc/calc.go b/calc/calc.go",
	"index "+calcBeforeBlob+".."+calcAfterBlob+" 100644",
	"--- a/calc/calc.go", "+++ b/calc/calc.go",
	"@@ -1 +1 @@", "-before", "+after",
)

var _ = Describe("DiffPatch with a source locator", func() {
	It("classifies the symbols of a modified file from the bytes the locator reads", func(ctx SpecContext) {
		locator := memoryLocator{blobs: map[string][]byte{calcBeforeBlob: []byte(patchCalcBefore), calcAfterBlob: []byte(patchCalcAfter)}}
		result, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(modifiedCalcPatch), Locator: locator, Visibility: VisibilityAll})
		Expect(err).To(Succeed())
		Expect([]string{result.RootKey, result.From.Commit, result.To.Commit}).To(Equal([]string{patchModule, PatchOld, PatchNew}))
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
		}))
	})

	It("reports import re-paths, formatting-only edits, and package-level var changes as their own rows", func(ctx SpecContext) {
		before := lines(
			"package calc", "",
			"import (", "\t\"fmt\"", "\t\"github.com/google/cel-go/cel\"", "\t\"os\"", ")", "",
			"var Env = cel.NewEnv(fmt.Sprint( 1))", "",
			"var limit = 3", "",
			"func Run() {", "\tif limit > 0 {os.Exit(1)}", "}", "",
			"func Keep() int { return 1 }",
		)
		after := lines(
			"package calc", "",
			"import (", "\t\"cel.dev/cel-go/cel\"", "\t\"fmt\"", "\t\"io\"", ")", "",
			"var Env = cel.NewEnv(fmt.Sprint(1))", "",
			"var limit = 4", "",
			"func Run() {", "\tif limit > 0 {", "\t\tio.EOF.Error()", "\t}", "}", "",
			"// Keep stays.", "func Keep() int { return 1 }",
		)
		locator := memoryLocator{blobs: map[string][]byte{calcBeforeBlob: []byte(before), calcAfterBlob: []byte(after)}}
		diff := func(changeTypes ...string) []patchRow {
			result, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(modifiedCalcPatch), Locator: locator, Visibility: VisibilityAll, ChangeTypes: changeTypes})
			Expect(err).To(Succeed())
			return patchRows(result)
		}
		Expect(diff()).To(Equal([]patchRow{
			{Path: "calc/calc.go", Class: "format", Name: "Env", Visibility: "exported", PathBefore: "calc/calc.go"},
			{Path: "calc/calc.go", Class: "format", Name: "Keep", Visibility: "exported", PathBefore: "calc/calc.go"},
			{Path: "calc/calc.go", Class: "body", Name: "Run", Visibility: "exported", PathBefore: "calc/calc.go"},
			{Path: "calc/calc.go", Class: "import", Name: "cel", Visibility: "internal", ShapeBefore: "github.com/google/cel-go/cel", ShapeAfter: "cel.dev/cel-go/cel", PathBefore: "calc/calc.go"},
			{Path: "calc/calc.go", Class: "import", Name: "io", Visibility: "internal", ShapeAfter: "io"},
			{Path: "calc/calc.go", Class: "body", Name: "limit", Visibility: "internal", PathBefore: "calc/calc.go"},
			{Path: "calc/calc.go", Class: "import", Name: "os", Visibility: "internal", ShapeBefore: "os", PathBefore: "calc/calc.go"},
		}))
		Expect(diff("!format", "!import")).To(Equal([]patchRow{
			{Path: "calc/calc.go", Class: "body", Name: "Run", Visibility: "exported", PathBefore: "calc/calc.go"},
			{Path: "calc/calc.go", Class: "body", Name: "limit", Visibility: "internal", PathBefore: "calc/calc.go"},
		}))
	})

	It("hides import rows from the default exported visibility", func(ctx SpecContext) {
		before := lines("package calc", "", "import \"github.com/google/cel-go/cel\"", "", "var env = cel.NewEnv")
		after := lines("package calc", "", "import \"cel.dev/cel-go/cel\"", "", "var env = cel.NewEnv")
		locator := memoryLocator{blobs: map[string][]byte{calcBeforeBlob: []byte(before), calcAfterBlob: []byte(after)}}
		result, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(modifiedCalcPatch), Locator: locator, Visibility: VisibilityExported})
		Expect(err).To(Succeed())
		Expect(patchRows(result)).To(BeEmpty())
		result, err = DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(modifiedCalcPatch), Locator: locator, Visibility: VisibilityInternal})
		Expect(err).To(Succeed())
		Expect(patchRows(result)).To(HaveLen(1))
	})

	It("fails with the locator's read error", func(ctx SpecContext) {
		readErr := errors.New("object store unreachable")
		_, err := DiffPatch(ctx, PatchOptions{Patch: strings.NewReader(modifiedCalcPatch), Locator: memoryLocator{readErr: readErr}, Visibility: VisibilityAll})
		Expect(err).To(MatchError(readErr))
	})
})
