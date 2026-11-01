package symboldiff

import (
	"fmt"
	"sort"

	"github.com/flanksource/uir/storage"
)

// importRows are the import rows of every file on both sides, when the visibility filter shows
// internal rows: one per alias whose import path changed or that only one side imports. Reordering an
// import block reports nothing. An added or removed file's imports belong to it and are not reported.
// Both sides must have been read; a diff reads them whenever these rows are shown.
func importRows(files []changedFile, markers coverageMarkers, filter rowFilter) ([]reportedRow, error) {
	if !filter.visibility.shows(ClassImport, importVisibility) {
		return nil, nil
	}
	var rows []reportedRow
	for _, file := range files {
		if !importsCompared(file) {
			continue
		}
		for _, side := range file.sides() {
			if !side.decoded {
				return nil, fmt.Errorf("the imports of %q are compared, but its %s document was not read", file.path, side.active.Document.ID)
			}
		}
		before, after := importPaths(file.before.content.Imports), importPaths(file.after.content.Imports)
		aliases := map[string]bool{}
		for alias := range before {
			aliases[alias] = true
		}
		for alias := range after {
			aliases[alias] = true
		}
		sorted := make([]string, 0, len(aliases))
		for alias := range aliases {
			sorted = append(sorted, alias)
		}
		sort.Strings(sorted)
		for _, alias := range sorted {
			old, current := before[alias], after[alias]
			if old == current {
				continue
			}
			row := Row{Class: ClassImport, Kind: "import", Name: alias, Visibility: importVisibility, ShapeBefore: old, ShapeAfter: current, Coverage: markers.of(file.path)}
			if old != "" {
				row.PathBefore = file.path
			}
			if current != "" {
				row.PathAfter = file.path
			}
			rows = append(rows, reportedRow{row: row, path: file.path, pkg: file.after.active.Source.PackagePath, visible: filter.changeTypes.shows(ClassImport)})
		}
	}
	return rows, nil
}

// importsCompared reports whether a file's imports are compared: it is on both sides and extracted.
func importsCompared(file changedFile) bool {
	return file.before != nil && file.after != nil && !file.excluded()
}

// importPaths maps each alias a file imports to its path. A blank or dot import binds no name, so it
// is keyed by its path as well.
func importPaths(imports []storage.DocumentImport) map[string]string {
	paths := make(map[string]string, len(imports))
	for _, imported := range imports {
		alias := imported.Alias
		if alias == "_" || alias == "." {
			alias += " " + imported.Path
		}
		paths[alias] = imported.Path
	}
	return paths
}
