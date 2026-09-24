package symboldiff

import (
	"fmt"
	"strings"

	"github.com/flanksource/clicky/api"
)

const (
	nameWidth       = 20
	classWidth      = 9
	shapeDiffIndent = "      "
)

var classStyles = map[Class]string{
	ClassAdded: "text-green-600", ClassRemoved: "text-red-600", ClassSignature: "text-yellow-600",
	ClassBody: "text-blue-600", ClassMoved: "text-cyan-600",
}

func (count *LineCount) String() string {
	return fmt.Sprintf("+%d -%d", count.Added, count.Removed)
}

// Pretty renders the diff: package, file, and symbol rows with their line counts, and a shape diff
// under each signature row. Its plain form is the "-"/"+" text of docs/diff.md; ANSI, markdown, and
// HTML carry token marks.
func (result Result) Pretty() api.Text {
	text := api.Text{}.Append(result.RootKey+" "+result.From.Commit+".."+result.To.Commit, "font-bold").Append("\n")
	if result.LinesError != "" {
		text = text.Append("line counts unavailable: "+result.LinesError, "text-red-600").Append("\n")
	}
	for _, pkg := range result.Packages {
		line := pkg.Path
		if result.Stat && pkg.Lines != nil {
			line += "  " + pkg.Lines.String()
		}
		text = text.Append(line, "font-bold").Append("\n")
		for _, file := range pkg.Files {
			text = text.Add(file.pretty(result.Stat))
		}
	}
	return text
}

func (file FileDiff) pretty(stat bool) api.Text {
	line := "  " + file.Path + " (" + string(file.Status) + ")" + markerSuffix(file.Coverage)
	switch {
	case stat && file.Lines != nil:
		line += "  " + file.Lines.String()
	case stat:
		line += "  lines unavailable: " + file.LinesError
	}
	if file.HiddenRows == 1 {
		line += "  (1 hidden row)"
	} else if file.HiddenRows > 1 {
		line += fmt.Sprintf("  (%d hidden rows)", file.HiddenRows)
	}
	text := api.Text{}.Append(line).Append("\n")
	for _, row := range file.Rows {
		text = text.Add(row.pretty(stat))
	}
	if stat && file.FileScope != nil {
		text = text.Append(strings.TrimRight(rowPrefix("(file scope)")+strings.Repeat(" ", classWidth)+" "+file.FileScope.String(), " ")).Append("\n")
	}
	return text
}

func (row Row) pretty(stat bool) api.Text {
	suffix := ""
	if stat && row.Lines != nil {
		suffix += " " + row.Lines.String()
	}
	suffix += markerSuffix(row.Coverage)
	if row.Class == ClassMoved || (row.PathBefore != "" && row.PathAfter != "" && row.PathBefore != row.PathAfter) {
		suffix += "  from " + row.PathBefore
	}
	if row.Note != "" {
		suffix += "  (" + row.Note + ")"
	}
	if shape := row.singleShape(); shape != "" {
		suffix += "  " + shape
	}
	class := string(row.Class)
	if suffix != "" {
		class = fmt.Sprintf("%-*s", classWidth, class)
	}
	text := api.Text{}.Append(rowPrefix(row.DisplayName())).Append(class, classStyles[row.Class]).Append(strings.TrimRight(suffix, " ")).Append("\n")
	if len(row.ShapeDiff) > 0 {
		text = text.Add(indentedShapeDiff{diff: row.ShapeDiff, indent: shapeDiffIndent})
	}
	return text
}

// singleShape is the one-line shape an added or removed row shows when it has no shape diff.
func (row Row) singleShape() string {
	if len(row.ShapeDiff) > 0 {
		return ""
	}
	var shape string
	switch row.Class {
	case ClassAdded:
		shape = row.ShapeAfter
	case ClassRemoved:
		shape = row.ShapeBefore
	}
	if first, _, multiline := strings.Cut(shape, "\n"); multiline {
		return first + " …"
	}
	return shape
}

func rowPrefix(name string) string {
	return fmt.Sprintf("    %-*s ", nameWidth, name)
}

func markerSuffix(markers []string) string {
	var suffix string
	for _, marker := range markers {
		suffix += " [" + marker + "]"
	}
	return suffix
}
