package symboldiff

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ParsePatch", func() {
	It("reads every git section's paths, blob ids and status, skipping mode-only changes", func() {
		patch := lines(
			"commit message text before the first section",
			"diff --git a/calc/calc.go b/calc/calc.go",
			"index 1a2b3c4..5d6e7f8 100644",
			"--- a/calc/calc.go",
			"+++ b/calc/calc.go",
			"@@ -1,2 +1,2 @@",
			" package calc",
			"--- not a header",
			"+++ not a header",
			"\\ No newline at end of file",
			"diff --git a/doc/new file.md b/doc/new file.md",
			"new file mode 100644",
			"index 0000000..e69de29",
			"diff --git a/old.go b/old.go",
			"deleted file mode 100644",
			"index 1111111111111111111111111111111111111111..0000000000000000000000000000000000000000",
			"--- a/old.go",
			"+++ /dev/null",
			"@@ -1 +0,0 @@",
			"-package calc",
			"diff --git a/calc/old_name.go b/calc/new_name.go",
			"similarity index 90%",
			"rename from calc/old_name.go",
			"rename to calc/new_name.go",
			"index 2222222..3333333 100644",
			"--- a/calc/old_name.go",
			"+++ b/calc/new_name.go",
			"@@ -1 +1,2 @@",
			" package calc",
			"+// moved",
			"diff --git a/run.sh b/run.sh",
			"old mode 100644",
			"new mode 100755",
			"diff --git a/logo.png b/logo.png",
			"index 4444444..5555555 100644",
			"Binary files a/logo.png and b/logo.png differ",
		)
		files, err := ParsePatch(strings.NewReader(patch))
		Expect(err).To(Succeed())
		Expect(files).To(Equal([]PatchFile{
			{OldPath: "calc/calc.go", NewPath: "calc/calc.go", OldBlob: "1a2b3c4", NewBlob: "5d6e7f8", Status: FileModified},
			{NewPath: "doc/new file.md", NewBlob: "e69de29", Status: FileAdded},
			{OldPath: "old.go", OldBlob: "1111111111111111111111111111111111111111", Status: FileRemoved},
			{OldPath: "calc/old_name.go", NewPath: "calc/new_name.go", OldBlob: "2222222", NewBlob: "3333333", Status: FileModified},
			{OldPath: "logo.png", NewPath: "logo.png", OldBlob: "4444444", NewBlob: "5555555", Status: FileModified},
		}))
	})

	It("reads a plain unified diff's paths as written, with no blob ids", func() {
		patch := lines("--- src/store.go", "+++ src/store.go", "@@ -1,2 +1,2 @@", "--- README.md", "+++ README.md", " unchanged", "--- /dev/null", "+++ ../consumer/model.go", "@@ -0,0 +1 @@", "+x")
		files, err := ParsePatch(strings.NewReader(patch))
		Expect(err).To(Succeed())
		Expect(files).To(Equal([]PatchFile{
			{OldPath: "src/store.go", NewPath: "src/store.go", Status: FileModified},
			{NewPath: "../consumer/model.go", Status: FileAdded},
		}))
	})

	DescribeTable("refuses a patch it cannot read",
		func(patch, message string) {
			_, err := ParsePatch(strings.NewReader(patch))
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("empty input", "", "patch has no file sections"),
		Entry("a rename without blob ids", lines("diff --git a/a.go b/b.go", "similarity index 100%", "rename from a.go", "rename to b.go"), `"a.go" -> "b.go" has no index line`),
		Entry("a truncated hunk", lines("--- a.go", "+++ a.go", "@@ -1,2 +1,2 @@", " package a"), "patch ended inside a hunk"),
		Entry("a combined merge diff", lines("diff --cc a.go"), "combined diff"),
	)
})
