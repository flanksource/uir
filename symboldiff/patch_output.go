package symboldiff

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

var outputHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type outputHunk struct {
	raw      []byte
	old, new int
}
type outputSection struct {
	file   PatchFile
	header []byte
	hunks  []outputHunk
}

func outputSections(raw []byte) ([]outputSection, error) {
	var sections []outputSection
	for _, chunk := range bytes.SplitAfter(raw, []byte("\n")) {
		if bytes.HasPrefix(chunk, []byte("diff --git ")) {
			sections = append(sections, outputSection{})
		}
		if len(sections) == 0 {
			if len(bytes.TrimSpace(chunk)) > 0 {
				return nil, fmt.Errorf("patch output requires Git file sections")
			}
			continue
		}
		section := &sections[len(sections)-1]
		if match := outputHunkHeader.FindSubmatch(chunk); match != nil {
			old, err := strconv.Atoi(string(match[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid old hunk position: %w", err)
			}
			newLine, err := strconv.Atoi(string(match[3]))
			if err != nil {
				return nil, fmt.Errorf("invalid new hunk position: %w", err)
			}
			section.hunks = append(section.hunks, outputHunk{old: old, new: newLine})
		}
		if len(section.hunks) == 0 {
			section.header = append(section.header, chunk...)
		} else {
			hunk := &section.hunks[len(section.hunks)-1]
			hunk.raw = append(hunk.raw, chunk...)
		}
	}
	for index := range sections {
		s := &sections[index]
		full := append([]byte{}, s.header...)
		for _, h := range s.hunks {
			full = append(full, h.raw...)
		}
		files, err := ParsePatch(bytes.NewReader(full))
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			s.file = files[0]
		}
	}
	return sections, nil
}

func selectedLines(rows []reportedRow, doc *fileSide, side int) map[int]bool {
	selected := map[int]bool{}
	if doc == nil {
		return selected
	}
	for _, row := range rows {
		if row.visible && row.row.Class == ClassImport && row.path == doc.path {
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, doc.path, doc.source, parser.ImportsOnly)
			if err != nil {
				panic(fmt.Sprintf("validated Go document %s has invalid imports: %v", doc.path, err))
			}
			{
				for _, spec := range parsed.Imports {
					value, err := strconv.Unquote(spec.Path.Value)
					if err != nil {
						panic(fmt.Sprintf("validated import in %s has invalid path: %v", doc.path, err))
					}
					if value == row.row.ShapeBefore || value == row.row.ShapeAfter {
						for line := fset.Position(spec.Pos()).Line; line <= fset.Position(spec.End()).Line; line++ {
							selected[line] = true
						}
					}
				}
			}
		}
		if !row.visible || row.pair == nil {
			continue
		}
		e := row.pair.before
		if side == 1 {
			e = row.pair.after
		}
		if e == nil || e.file != doc {
			continue
		}
		span := e.symbol.ExtentBytes
		start := bytes.Count(doc.source[:span[0]], []byte("\n")) + 1
		end := bytes.Count(doc.source[:span[1]], []byte("\n")) + 1
		for line := start; line <= end; line++ {
			selected[line] = true
		}
		lines := strings.Split(string(doc.source), "\n")
		for line := start - 1; line > 0 && strings.TrimSpace(lines[line-1]) == ""; line-- {
			selected[line] = true
		}
		for line := end + 1; line <= len(lines) && strings.TrimSpace(lines[line-1]) == ""; line++ {
			selected[line] = true
		}
	}
	return selected
}

func wholeFileSelected(rows []reportedRow, doc *fileSide, side int) bool {
	if doc == nil || len(doc.content.Symbols) == 0 {
		return false
	}
	selected := selectedLines(rows, doc, side)
	for index := range doc.owners {
		symbol := doc.content.Symbols[index]
		if !selected[bytes.Count(doc.source[:symbol.NameBytes[0]], []byte("\n"))+1] {
			return false
		}
	}
	return true
}

func selectPatch(raw []byte, files []changedFile, rows []reportedRow) ([]byte, error) {
	sections, err := outputSections(raw)
	if err != nil {
		return nil, err
	}
	byPath := map[string]changedFile{}
	for _, file := range files {
		byPath[file.path] = file
	}
	refs := referenceLines(files, rows)
	output := []byte{}
	for _, section := range sections {
		docs := [2]*fileSide{byPath[section.file.OldPath].before, byPath[section.file.NewPath].after}
		if docs[0] == nil && docs[1] == nil {
			continue
		}
		selected := [2]map[int]bool{selectedLines(rows, docs[0], 0), selectedLines(rows, docs[1], 1)}
		for side, doc := range docs {
			if doc == nil {
				continue
			}
			for _, row := range rows {
				if row.visible || row.pair == nil {
					continue
				}
				e := row.pair.before
				if side == 1 {
					e = row.pair.after
				}
				if e == nil || e.file != doc || !e.owner() {
					continue
				}
				start := bytes.Count(doc.source[:e.symbol.ExtentBytes[0]], []byte("\n")) + 1
				end := bytes.Count(doc.source[:e.symbol.ExtentBytes[1]], []byte("\n")) + 1
				for line := start; line <= end; line++ {
					if selected[side][line] {
						return nil, fmt.Errorf("%s: mixed hunk selection shares line %d with filtered declaration %s", doc.path, line, row.row.DisplayName())
					}
				}
			}
		}
		whole := [2]bool{docs[1] == nil && wholeFileSelected(rows, docs[0], 0), docs[0] == nil && wholeFileSelected(rows, docs[1], 1)}
		var kept []byte
		for _, hunk := range section.hunks {
			keep, err := selectHunk(hunk, docs, selected, whole, refs)
			if err != nil {
				return nil, fmt.Errorf("%s -> %s: %w", section.file.OldPath, section.file.NewPath, err)
			}
			if keep {
				kept = append(kept, hunk.raw...)
			}
		}
		if len(kept) > 0 {
			if bytes.Contains(section.header, []byte("\nold mode ")) {
				return nil, fmt.Errorf("%s: selected content also changes file mode", section.file.OldPath)
			}
			output = append(output, section.header...)
			output = append(output, kept...)
		}
	}
	return output, nil
}

func selectHunk(hunk outputHunk, docs [2]*fileSide, selected [2]map[int]bool, whole [2]bool, refs [2]map[*fileSide]map[int]string) (bool, error) {
	positions := [2]int{hunk.old, hunk.new}
	var sources [2][]string
	for side, doc := range docs {
		if doc != nil {
			sources[side] = strings.Split(string(doc.source), "\n")
		}
	}
	chosen, other := false, false
	refChanges := [2]map[string]int{{}, {}}
	for index, raw := range bytes.SplitAfter(hunk.raw, []byte("\n")) {
		if index == 0 || len(raw) == 0 || raw[0] == '\\' {
			continue
		}
		op := raw[0]
		if op != ' ' && op != '-' && op != '+' {
			return false, fmt.Errorf("invalid hunk body")
		}
		for side := range 2 {
			if side == 0 && op == '+' || side == 1 && op == '-' {
				continue
			}
			line := positions[side]
			if line < 1 || line > len(sources[side]) || sources[side][line-1] != string(bytes.TrimSuffix(raw[1:], []byte("\n"))) {
				return false, fmt.Errorf("hunk at -%d +%d does not match source line %d", hunk.old, hunk.new, line)
			}
			positions[side]++
			if op == ' ' {
				continue
			}
			if whole[side] || selected[side][line] {
				chosen = true
			} else if normalized, ok := refs[side][docs[side]][line]; ok {
				refChanges[side][normalized]++
			} else {
				other = true
			}
		}
	}
	for side := range 2 {
		for line, count := range refChanges[side] {
			if count != refChanges[1-side][line] {
				return false, fmt.Errorf("mixed hunk at -%d +%d has an unproven or mixed rename reference", hunk.old, hunk.new)
			}
			chosen = true
		}
	}
	if chosen && other {
		return false, fmt.Errorf("mixed hunk at -%d +%d contains selected and unrelated changes; produce smaller hunks with git diff -U0", hunk.old, hunk.new)
	}
	return chosen, nil
}
