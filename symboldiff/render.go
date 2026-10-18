package symboldiff

import (
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/uir"
)

type changeTheme struct {
	icon  icons.Icon
	style string
}

var classThemes = map[Class]changeTheme{
	ClassAdded:     {icon: icons.Add, style: uir.StyleDiffAdded},
	ClassRemoved:   {icon: icons.Delete, style: uir.StyleDiffRemoved},
	ClassSignature: {icon: icons.Edit, style: uir.StyleDiffSignature},
	ClassStructure: {icon: icons.Refactor, style: uir.StyleDiffStructure},
	ClassBody:      {icon: icons.Code, style: uir.StyleDiffBody},
	ClassMoved:     {icon: icons.Rename, style: uir.StyleDiffMoved},
}

var fileThemes = map[FileStatus]changeTheme{
	FileAdded:    classThemes[ClassAdded],
	FileRemoved:  classThemes[ClassRemoved],
	FileModified: classThemes[ClassSignature],
}

func (count *LineCount) String() string {
	return fmt.Sprintf("+%d -%d", count.Added, count.Removed)
}

func lineCountText(count *LineCount) api.Text {
	return api.Text{}.Append(fmt.Sprintf("+%d", count.Added), uir.StyleDiffAddedCount).
		Append(" ").Append(fmt.Sprintf("-%d", count.Removed), uir.StyleDiffRemovedCount)
}

// Pretty renders the diff as a package, file, symbol, and shape tree.
func (result Result) Pretty() api.Text {
	children := make([]api.TextTree, 0, len(result.Packages)+1)
	if result.LinesError != "" {
		children = append(children, clicky.Tree(api.Text{}.Add(icons.Warning).Append(" line counts unavailable: "+result.LinesError, uir.StyleWarning)))
	}
	for _, pkg := range result.Packages {
		children = append(children, pkg.tree(result.Stat))
	}
	label := api.Text{}.Append(result.RootKey+" "+result.From.Commit+".."+result.To.Commit, uir.StyleBold)
	return api.Text{}.Add(clicky.Tree(label, children...)).Append("\n")
}

func (pkg PackageDiff) tree(stat bool) api.TextTree {
	label := api.Text{}.Add(icons.Package).Append(" ").Append(pkg.Path, uir.StyleBold)
	if stat && pkg.Lines != nil {
		label = label.Append("  ").Add(lineCountText(pkg.Lines))
	}
	children := make([]api.TextTree, 0, len(pkg.Files))
	for _, file := range pkg.Files {
		children = append(children, file.tree(stat))
	}
	return clicky.Tree(label, children...)
}

func (file FileDiff) tree(stat bool) api.TextTree {
	theme, ok := fileThemes[file.Status]
	if !ok {
		panic(fmt.Sprintf("unknown file diff status %q for %s", file.Status, file.Path))
	}
	label := api.Text{}.Add(theme.icon.WithStyle(theme.style)).Append(" ").Append(file.Path, uir.StyleFileName).
		Append(" (").Append(string(file.Status), theme.style).Append(")").Append(markerSuffix(file.Coverage))
	switch {
	case stat && file.Lines != nil:
		label = label.Append("  ").Add(lineCountText(file.Lines))
	case stat && file.LinesError != "":
		label = label.Append("  lines unavailable: "+file.LinesError, uir.StyleWarning)
	}
	if file.HiddenRows == 1 {
		label = label.Append("  (1 hidden row)", uir.StyleMuted)
	} else if file.HiddenRows > 1 {
		label = label.Append(fmt.Sprintf("  (%d hidden rows)", file.HiddenRows), uir.StyleMuted)
	}
	children := make([]api.TextTree, 0, len(file.Rows)+1)
	for _, row := range file.Rows {
		children = append(children, row.tree(stat))
	}
	if stat && file.FileScope != nil {
		children = append(children, clicky.Tree(api.Text{}.Append("(file scope)", uir.StyleMuted).
			Append("  ").Add(lineCountText(file.FileScope))))
	}
	return clicky.Tree(label, children...)
}

func (row Row) tree(stat bool) api.TextTree {
	theme, ok := classThemes[row.Class]
	if !ok {
		panic(fmt.Sprintf("unknown symbol diff class %q for %s", row.Class, row.DisplayName()))
	}
	label := api.Text{}.Add(theme.icon.WithStyle(theme.style)).Append(" ").Append(row.DisplayName(), uir.StyleName).
		Append("  ").Append(string(row.Class), theme.style)
	if stat && row.Lines != nil {
		label = label.Append("  ").Add(lineCountText(row.Lines))
	}
	label = label.Append(markerSuffix(row.Coverage))
	switch {
	case row.MovedAway:
		label = label.Append("  to "+row.PathAfter, uir.StyleDim)
	case row.Class == ClassMoved || (row.PathBefore != "" && row.PathAfter != "" && row.PathBefore != row.PathAfter):
		label = label.Append("  from "+row.PathBefore, uir.StyleDim)
	}
	if row.Note != "" {
		label = label.Append("  ("+row.Note+")", uir.StyleDim)
	}
	if shape := row.singleShape(); shape != "" {
		label = label.Append("  " + shape)
	}
	if len(row.ShapeDiff) > 0 {
		return clicky.Tree(label, clicky.Tree(indentedShapeDiff{diff: row.ShapeDiff}))
	}
	return clicky.Tree(label)
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

func markerSuffix(markers []string) string {
	var suffix string
	for _, marker := range markers {
		suffix += " [" + marker + "]"
	}
	return suffix
}
