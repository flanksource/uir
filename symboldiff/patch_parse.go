package symboldiff

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// PatchFile is one file section of a unified diff. The blobs are the object ids of a git patch's index
// line, abbreviated or full, and empty in a plain unified diff; an absent side has no path and no blob.
type PatchFile struct {
	OldPath, NewPath string
	OldBlob, NewBlob string
	Status           FileStatus
}

const absentPath = "/dev/null"

// maxPatchLine bounds one line of a patch; a minified file's line can be long.
const maxPatchLine = 64 << 20

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

// patchSection is the file section being read: a git section opens with "diff --git" and carries
// extended headers before its "---"/"+++" lines, a plain one opens with "---".
type patchSection struct {
	file                         PatchFile
	git, indexed, moded          bool
	added, removed               bool
	oldHeader, newHeader, hunked bool
}

type patchParser struct {
	files              []PatchFile
	section            *patchSection
	sections           int
	oldLines, newLines int
}

// ParsePatch reads the file sections of a git patch or a plain unified diff. Hunk bodies are counted
// and skipped, text before the first section is ignored, and a section that only changes a file's
// mode is left out.
func ParsePatch(patch io.Reader) ([]PatchFile, error) {
	scanner := bufio.NewScanner(patch)
	scanner.Buffer(nil, maxPatchLine)
	parser := &patchParser{}
	for number := 1; scanner.Scan(); number++ {
		if err := parser.line(scanner.Text()); err != nil {
			return nil, fmt.Errorf("patch line %d: %w", number, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read patch: %w", err)
	}
	return parser.finish()
}

func (parser *patchParser) line(line string) error {
	switch {
	case parser.oldLines > 0 || parser.newLines > 0:
		return parser.hunkLine(line)
	case strings.HasPrefix(line, "diff --git "):
		return parser.start(&patchSection{git: true, file: gitHeaderPaths(strings.TrimPrefix(line, "diff --git "))})
	case strings.HasPrefix(line, "diff --cc ") || strings.HasPrefix(line, "diff --combined "):
		return errors.New("a combined diff of a merge is not supported; diff against one parent")
	case strings.HasPrefix(line, "--- "):
		if section := parser.section; section == nil || section.oldHeader || section.hunked {
			if err := parser.start(&patchSection{}); err != nil {
				return err
			}
		}
		return parser.section.pathHeader(line, &parser.section.file.OldPath, "a/", &parser.section.added)
	case strings.HasPrefix(line, "+++ "):
		if parser.section == nil || !parser.section.oldHeader || parser.section.newHeader {
			return fmt.Errorf("%q does not follow a --- line", line)
		}
		parser.section.newHeader = true
		return parser.section.pathHeader(line, &parser.section.file.NewPath, "b/", &parser.section.removed)
	case strings.HasPrefix(line, "@@ "):
		return parser.hunkHeader(line)
	case parser.section != nil && parser.section.git && !parser.section.oldHeader:
		return parser.section.extendedHeader(line)
	}
	return nil
}

// start closes the current section and opens the next.
func (parser *patchParser) start(next *patchSection) error {
	if err := parser.flush(); err != nil {
		return err
	}
	parser.section = next
	parser.sections++
	return nil
}

func (parser *patchParser) flush() error {
	if parser.section == nil {
		return nil
	}
	file, keep, err := parser.section.result()
	if err != nil {
		return err
	}
	if keep {
		parser.files = append(parser.files, file)
	}
	parser.section = nil
	return nil
}

func (parser *patchParser) finish() ([]PatchFile, error) {
	if parser.oldLines > 0 || parser.newLines > 0 {
		return nil, errors.New("patch ended inside a hunk")
	}
	if err := parser.flush(); err != nil {
		return nil, err
	}
	if parser.sections == 0 {
		return nil, errors.New("patch has no file sections; pipe the output of git diff")
	}
	return parser.files, nil
}

func (parser *patchParser) hunkHeader(line string) error {
	match := hunkHeader.FindStringSubmatch(line)
	switch {
	case match == nil:
		return fmt.Errorf("invalid hunk header %q", line)
	case parser.section == nil || !parser.section.newHeader:
		return fmt.Errorf("hunk %q is outside a file section", line)
	}
	parser.section.hunked = true
	parser.oldLines, parser.newLines = 1, 1
	for index, count := range []*int{&parser.oldLines, &parser.newLines} {
		if match[index+1] == "" {
			continue
		}
		value, err := strconv.Atoi(match[index+1])
		if err != nil {
			return fmt.Errorf("invalid hunk size in %q: %w", line, err)
		}
		*count = value
	}
	return nil
}

func (parser *patchParser) hunkLine(line string) error {
	if line == "" {
		return errors.New("patch has an empty line inside a hunk")
	}
	switch line[0] {
	case ' ':
		parser.oldLines--
		parser.newLines--
	case '-':
		parser.oldLines--
	case '+':
		parser.newLines--
	case '\\':
		return nil
	default:
		return fmt.Errorf("invalid hunk line %q", line)
	}
	if parser.oldLines < 0 || parser.newLines < 0 {
		return errors.New("hunk exceeds its declared line count")
	}
	return nil
}

// pathHeader reads a ---/+++ path: /dev/null marks the side absent, so the file was added or removed.
func (section *patchSection) pathHeader(line string, path *string, prefix string, absent *bool) error {
	section.oldHeader = true
	value, _, _ := strings.Cut(line[4:], "\t")
	value, err := unquotePath(value)
	if err != nil {
		return err
	}
	if value == absentPath {
		*absent = true
		return nil
	}
	if section.git {
		value = strings.TrimPrefix(value, prefix)
	}
	*path = value
	return nil
}

// extendedHeader reads the git headers between "diff --git" and "---".
func (section *patchSection) extendedHeader(line string) error {
	header, value, _ := strings.Cut(line, " ")
	switch {
	case header == "index":
		return section.index(value)
	case strings.HasPrefix(line, "new file mode "):
		section.added = true
	case strings.HasPrefix(line, "deleted file mode "):
		section.removed = true
	case strings.HasPrefix(line, "old mode ") || strings.HasPrefix(line, "new mode "):
		section.moded = true
	case strings.HasPrefix(line, "copy from ") || strings.HasPrefix(line, "copy to "):
		return fmt.Errorf("copy %q is not supported; produce the patch without -C", line)
	case strings.HasPrefix(line, "rename from "):
		return assignPath(&section.file.OldPath, strings.TrimPrefix(line, "rename from "))
	case strings.HasPrefix(line, "rename to "):
		return assignPath(&section.file.NewPath, strings.TrimPrefix(line, "rename to "))
	}
	return nil
}

func (section *patchSection) index(value string) error {
	ids, _, _ := strings.Cut(value, " ")
	old, current, found := strings.Cut(ids, "..")
	if !found || old == "" || current == "" {
		return fmt.Errorf("invalid index line %q", "index "+value)
	}
	section.indexed = true
	section.file.OldBlob, section.file.NewBlob = presentBlob(old), presentBlob(current)
	return nil
}

// presentBlob is the id of a present side; git writes an absent side as all zeros.
func presentBlob(id string) string {
	if strings.Trim(id, "0") == "" {
		return ""
	}
	return id
}

// result is the section's file, or keep=false for a mode-only change.
func (section *patchSection) result() (PatchFile, bool, error) {
	file := section.file
	if section.git && !section.indexed {
		if section.moded && !section.oldHeader {
			return PatchFile{}, false, nil
		}
		return PatchFile{}, false, fmt.Errorf("%q -> %q has no index line to read its blobs from; a rename without content changes needs git diff --no-renames", file.OldPath, file.NewPath)
	}
	file.Status = FileModified
	switch {
	case section.added:
		file.Status, file.OldPath = FileAdded, ""
	case section.removed:
		file.Status, file.NewPath = FileRemoved, ""
	}
	if file.OldPath == "" && file.NewPath == "" {
		return PatchFile{}, false, errors.New("a patch section names no path")
	}
	return file, true, nil
}

// gitHeaderPaths reads "a/<path> b/<path>". Only a header whose two paths are equal, or quoted, can be
// split unambiguously; a rename's paths come from its rename and ---/+++ lines.
func gitHeaderPaths(header string) PatchFile {
	if strings.HasPrefix(header, `"`) {
		old, err := strconv.QuotedPrefix(header)
		if err != nil {
			return PatchFile{}
		}
		oldPath, _ := strconv.Unquote(old)
		newPath, err := unquotePath(strings.TrimPrefix(header[len(old):], " "))
		if err != nil {
			return PatchFile{}
		}
		return PatchFile{OldPath: strings.TrimPrefix(oldPath, "a/"), NewPath: strings.TrimPrefix(newPath, "b/")}
	}
	half := (len(header) - 1) / 2
	if len(header)%2 == 0 || header[half] != ' ' || !strings.HasPrefix(header, "a/") || header[2:half] != strings.TrimPrefix(header[half+1:], "b/") {
		return PatchFile{}
	}
	return PatchFile{OldPath: header[2:half], NewPath: header[2:half]}
}

func assignPath(path *string, value string) error {
	unquoted, err := unquotePath(value)
	*path = unquoted
	return err
}

// unquotePath reads a path git quoted because it holds special characters.
func unquotePath(value string) (string, error) {
	if !strings.HasPrefix(value, `"`) {
		return value, nil
	}
	unquoted, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("invalid quoted path %s: %w", value, err)
	}
	return unquoted, nil
}
