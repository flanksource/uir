// Package symboldiff compares the symbol index of two commits of one module root: which symbols
// were added, removed, or changed in signature or body, and, from hash-verified Git blobs, how many
// lines each symbol gained and lost. See docs/diff.md.
package symboldiff

import (
	"fmt"
	"strings"
)

// Visibility selects which rows a diff reports. A row is kept when the symbol's visibility on either
// side matches.
type Visibility string

const (
	VisibilityExported Visibility = "exported"
	VisibilityInternal Visibility = "internal"
	VisibilityAll      Visibility = "all"
)

// ParseVisibility validates a --visibility value.
func ParseVisibility(value string) (Visibility, error) {
	switch visibility := Visibility(value); visibility {
	case VisibilityExported, VisibilityInternal, VisibilityAll:
		return visibility, nil
	}
	return "", fmt.Errorf("visibility %q is not one of exported, internal, or all", value)
}

// Class is how a symbol changed between the two commits.
type Class string

const (
	ClassAdded     Class = "added"
	ClassRemoved   Class = "removed"
	ClassSignature Class = "signature"
	ClassBody      Class = "body"
	ClassMoved     Class = "moved"
)

// Coverage markers attached to files and rows whose documents did not prove every fact.
const (
	MarkerPartial  = "partial"
	MarkerSyntax   = "syntax"
	MarkerExcluded = "excluded"
	MarkerManifest = "manifest"
)

// NoteLayoutOnly marks a body row whose hashes are equal but whose lines changed: comments or layout.
const NoteLayoutOnly = "comments or layout only; body_hash unchanged"

// Options selects the root, the two commits, and what the diff reports.
type Options struct {
	RootKey      string
	From         string
	To           string
	SnapshotFrom string
	SnapshotTo   string
	Visibility   Visibility
	Stat         bool
}

// ParseRange splits "<from>..<to>" into its two commits; both must be non-empty.
func ParseRange(spec string) (string, string, error) {
	from, to, found := strings.Cut(spec, "..")
	if !found || strings.HasPrefix(to, ".") {
		return "", "", fmt.Errorf("commit range %q is not <from>..<to>", spec)
	}
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return "", "", fmt.Errorf("commit range %q has an empty revision", spec)
	}
	return from, to, nil
}

// Side is one end of the diff: the resolved commit and the snapshot selected for it.
type Side struct {
	Commit        string `json:"commit"`
	SnapshotID    string `json:"snapshot_id"`
	WorktreeState string `json:"worktree_state"`
	Checkout      string `json:"checkout,omitempty"`
}

// LineCount is the number of lines added and removed.
type LineCount struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

func (count *LineCount) add(other LineCount) {
	count.Added += other.Added
	count.Removed += other.Removed
}

// Result is a commit-to-commit diff grouped by package and file.
// LinesError is set when a commit has no registered checkout: every file then carries it too, and
// manifests cannot be compared.
type Result struct {
	RootKey    string        `json:"root_key"`
	From       Side          `json:"from"`
	To         Side          `json:"to"`
	Visibility Visibility    `json:"visibility"`
	Stat       bool          `json:"stat"`
	LinesError string        `json:"lines_error,omitempty"`
	Packages   []PackageDiff `json:"packages"`
}

// PackageDiff is one package's changed files. Lines is the sum of its files, and is absent when any
// file's line counts failed.
type PackageDiff struct {
	Path  string     `json:"path"`
	Lines *LineCount `json:"lines,omitempty"`
	Files []FileDiff `json:"files"`
}

// FileStatus says whether a file exists on one side only or on both.
type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileRemoved  FileStatus = "removed"
	FileModified FileStatus = "modified"
)

// FileDiff is one changed file: its symbol rows and, with Stat, its line counts. Lines is the sum of
// every row reported under the file, hidden ones included, plus FileScope. LinesError states why the
// line counts could not be computed; the rows are still reported.
type FileDiff struct {
	Path       string     `json:"path"`
	Status     FileStatus `json:"status"`
	Coverage   []string   `json:"coverage,omitempty"`
	Excluded   string     `json:"excluded,omitempty"`
	Lines      *LineCount `json:"lines,omitempty"`
	FileScope  *LineCount `json:"file_scope,omitempty"`
	LinesError string     `json:"lines_error,omitempty"`
	HiddenRows int        `json:"hidden_rows,omitempty"`
	Rows       []Row      `json:"rows"`
}

// Row is one changed symbol. ShapeDiff is set for signature rows and for the added half of a
// removed-plus-added pair (Group), where it diffs the old identity's shape against the new one.
type Row struct {
	Class       Class      `json:"class"`
	Kind        string     `json:"kind"`
	Owner       string     `json:"owner,omitempty"`
	Name        string     `json:"name"`
	Visibility  string     `json:"visibility"`
	ShapeBefore string     `json:"shape_before,omitempty"`
	ShapeAfter  string     `json:"shape_after,omitempty"`
	ShapeDiff   ShapeDiff  `json:"shape_diff,omitempty"`
	PathBefore  string     `json:"path_before,omitempty"`
	PathAfter   string     `json:"path_after,omitempty"`
	Lines       *LineCount `json:"lines,omitempty"`
	Coverage    []string   `json:"coverage,omitempty"`
	Group       string     `json:"group,omitempty"`
	Note        string     `json:"note,omitempty"`
}

// DisplayName is Owner.Name, or Name for a package-level symbol.
func (row Row) DisplayName() string {
	if row.Owner == "" {
		return row.Name
	}
	return row.Owner + "." + row.Name
}
