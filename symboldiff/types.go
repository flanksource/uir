// Package symboldiff compares the symbol index of two revisions of one module root: which symbols
// were added, removed, or changed in signature, structure, or body, and, from hash-verified Git blobs
// (or an external root's stored blobs), how many lines each symbol gained and lost. See docs/diff.md.
package symboldiff

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/commons/collections"
)

// Visibility selects which rows a diff reports. A row is kept when the symbol's visibility on either
// side matches, except a structure row, which only internal and all report.
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

// shows reports whether the filter keeps a row of the class for a symbol of the given visibility. A
// structure row changes only what code inside the symbol's visibility boundary can observe, so it is
// shown with internal or all whatever the symbol's visibility, and never with exported.
func (visibility Visibility) shows(class Class, symbol string) bool {
	switch {
	case visibility == VisibilityAll:
		return true
	case class == ClassStructure:
		return visibility == VisibilityInternal
	}
	return symbol == string(visibility)
}

// ChangeTypes selects the row classes a diff reports, as commons collections.MatchItems patterns: a
// class name, * wildcards, and ! exclusions, which take precedence. Empty reports every class. It
// filters rows after symbols are paired, so it never changes how a symbol is classified.
type ChangeTypes []string

var classes = []Class{ClassAdded, ClassRemoved, ClassSignature, ClassStructure, ClassBody, ClassMoved}

// ParseChangeTypes validates --change-type patterns: each, without its !, must match at least one class.
func ParseChangeTypes(patterns []string) (ChangeTypes, error) {
	for _, pattern := range patterns {
		bare := strings.TrimPrefix(strings.TrimSpace(pattern), "!")
		if bare == "" || !slices.ContainsFunc(classes, func(class Class) bool { return collections.MatchItems(string(class), bare) }) {
			return nil, fmt.Errorf("change type %q matches none of %s", pattern, joinClasses())
		}
	}
	return ChangeTypes(patterns), nil
}

func joinClasses() string {
	names := make([]string, len(classes))
	for index, class := range classes {
		names[index] = string(class)
	}
	return strings.Join(names, ", ")
}

func (types ChangeTypes) shows(class Class) bool {
	return collections.MatchItems(string(class), types...)
}

// rowFilter is what decides whether a row is reported or counted as hidden.
type rowFilter struct {
	visibility  Visibility
	changeTypes ChangeTypes
}

// Class is how a symbol changed between the two commits. Of a symbol on both sides, the class names the
// outermost layer whose hash differs: signature the public layer, structure the private layer only,
// body the content layer only.
type Class string

const (
	ClassAdded     Class = "added"
	ClassRemoved   Class = "removed"
	ClassSignature Class = "signature"
	ClassStructure Class = "structure"
	ClassBody      Class = "body"
	ClassMoved     Class = "moved"
)

// changedClass is the class of a symbol declared on both sides from which of its layers differ and
// whether its defining path changed; empty when nothing did.
func changedClass(public, private, content, moved bool) Class {
	switch {
	case public:
		return ClassSignature
	case private:
		return ClassStructure
	case content:
		return ClassBody
	case moved:
		return ClassMoved
	}
	return ""
}

// Coverage markers attached to files and rows whose documents did not prove every fact.
const (
	MarkerPartial  = "partial"
	MarkerSyntax   = "syntax"
	MarkerExcluded = "excluded"
	MarkerManifest = "manifest"
)

// NoteLayoutOnly marks a body row whose hashes are equal but whose lines changed: comments or layout.
const NoteLayoutOnly = "comments or layout only; content_hash unchanged"

// Options selects the root, the revisions to compare, and what the diff reports.
type Options struct {
	RootKey string
	// Revisions is what git diff takes: one spec (A..B, A.., ..B, A...B, A^!, A^-n, or a single
	// revision, compared against the worktree) or two revisions. pr:N names a pull request's head.
	Revisions    []string
	SnapshotFrom string
	SnapshotTo   string
	Visibility   Visibility
	ChangeTypes  ChangeTypes
	Stat         bool
	AutoIndex    bool
	IncludeTests bool
	// TaskContext bounds the module-index run AutoIndex starts for a missing commit snapshot. The run
	// outlives the diff's own context, which bounds only the wait, so a diff that goes away neither
	// stops an index another diff of the commit is waiting on nor leaves it half done.
	TaskContext context.Context
	// Location is the registered checkout to resolve and index commits from; empty means the primary
	// checkout first, then any other that has the commit.
	Location string
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
	RootKey     string        `json:"root_key"`
	From        Side          `json:"from"`
	To          Side          `json:"to"`
	Visibility  Visibility    `json:"visibility"`
	ChangeTypes ChangeTypes   `json:"change_types,omitempty"`
	Stat        bool          `json:"stat"`
	LinesError  string        `json:"lines_error,omitempty"`
	Packages    []PackageDiff `json:"packages"`
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

// Row is one changed symbol. ShapeDiff is set for signature rows, over the public shapes; for structure
// rows, over the private shapes; and for the added half of a removed-plus-added pair (Group), where it
// diffs the old identity's shape against the new one.
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
	// MovedAway marks the copy of a row reported under the file a symbol left: a symbol declared in
	// another file on the new side is reported under both files. Its lines count under the new file only.
	MovedAway bool `json:"moved_away,omitempty"`
}

// DisplayName is Owner.Name, or Name for a package-level symbol.
func (row Row) DisplayName() string {
	if row.Owner == "" {
		return row.Name
	}
	return row.Owner + "." + row.Name
}
