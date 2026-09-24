package indexer

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/flanksource/uir/storage"
	"golang.org/x/tools/go/packages"
)

// packageDiagnostic is one load or type error of a package, positioned in one of its files when the
// toolchain reported a position there. Messages are made relative to the module root so that a
// document does not depend on where the checkout lives.
type packageDiagnostic struct {
	Path    string         `json:"path,omitempty"`
	Range   *storage.Range `json:"range,omitempty"`
	Message string         `json:"message"`
}

func (load *typedLoad) diagnostic(loadError packages.Error, files []discoveredFile) packageDiagnostic {
	diagnostic := packageDiagnostic{Message: load.relative(loadError.Msg)}
	name, line, column, positioned := parsePosition(loadError.Pos)
	if name == "" {
		return diagnostic
	}
	for _, file := range files {
		if file.AbsolutePath == name {
			diagnostic.Path = file.PathKey
			if positioned {
				diagnostic.Range = positionRange(file.Content, line, column)
			}
			return diagnostic
		}
	}
	if relative, err := filepath.Rel(load.root.LocalPath, name); err == nil && filepath.IsLocal(relative) {
		diagnostic.Path = filepath.ToSlash(relative)
	}
	return diagnostic
}

func (load *typedLoad) relative(message string) string {
	message = strings.ReplaceAll(message, load.root.LocalPath+string(filepath.Separator), "")
	return strings.ReplaceAll(message, load.root.LocalPath, ".")
}

// parsePosition splits a go/packages position, "file:line:col", "file:line", or "file".
func parsePosition(position string) (string, int, int, bool) {
	if position == "" || position == "-" {
		return "", 0, 0, false
	}
	rest, last, found := cutTrailingNumber(position)
	if !found {
		return position, 0, 0, false
	}
	name, line, found := cutTrailingNumber(rest)
	if !found {
		return rest, last, 1, true
	}
	return name, line, last, true
}

func cutTrailingNumber(value string) (string, int, bool) {
	index := strings.LastIndexByte(value, ':')
	if index < 0 {
		return value, 0, false
	}
	number, err := strconv.Atoi(value[index+1:])
	if err != nil {
		return value, 0, false
	}
	return value[:index], number, true
}

// positionRange is the one-byte range at a one-based line and byte column, or nil when the file
// has no such position.
func positionRange(content []byte, line, column int) *storage.Range {
	lines := newLineIndex(content)
	if len(content) == 0 || line < 1 || line > len(lines.starts) || column < 1 {
		return nil
	}
	start := min(lines.starts[line-1]+column-1, len(content)-1)
	value := lines.rangeOf(storage.ByteSpan{start, start + 1})
	return &value
}

// fileDiagnostics are the positioned diagnostics of one file, in the document's format.
func fileDiagnostics(diagnostics []packageDiagnostic, pathKey string) []storage.DocumentDiagnostic {
	result := []storage.DocumentDiagnostic{}
	for _, diagnostic := range diagnostics {
		if diagnostic.Path == pathKey && diagnostic.Range != nil {
			result = append(result, storage.DocumentDiagnostic{Range: *diagnostic.Range, Message: diagnostic.Message})
		}
	}
	return result
}

// uniqueDiagnostics drops the repeats a package's test variants report for the same error.
func uniqueDiagnostics(diagnostics []packageDiagnostic) []packageDiagnostic {
	seen := map[packageDiagnostic]bool{}
	unique := make([]packageDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		key := diagnostic
		key.Range = nil
		if diagnostic.Range != nil {
			key.Message += "@" + strconv.Itoa(diagnostic.Range[0]) + ":" + strconv.Itoa(diagnostic.Range[1])
		}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, diagnostic)
		}
	}
	return unique
}

var coverageStrength = map[storage.Coverage]int{
	storage.CoverageIndexed: 3, storage.CoveragePartial: 2, storage.CoverageSyntax: 1, storage.CoverageExcluded: 0,
}

// weakestCoverage is a snapshot's coverage: the weakest of its extracted packages'. Excluded
// packages were deliberately not extracted, so they weaken it only when nothing else was extracted.
func weakestCoverage(coverages []storage.Coverage) storage.Coverage {
	weakest, extracted := storage.CoverageIndexed, false
	for _, coverage := range coverages {
		if coverage == storage.CoverageExcluded {
			continue
		}
		extracted = true
		if coverageStrength[coverage] < coverageStrength[weakest] {
			weakest = coverage
		}
	}
	if !extracted && len(coverages) > 0 {
		return storage.CoverageExcluded
	}
	return weakest
}
