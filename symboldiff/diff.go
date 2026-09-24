package symboldiff

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// selectedSide is one commit and the snapshot selected for it.
type selectedSide struct {
	commit   string
	snapshot storage.ModuleSnapshot
}

func (side selectedSide) info() Side {
	return Side{Commit: side.commit, SnapshotID: side.snapshot.ID.String(), WorktreeState: string(side.snapshot.WorktreeState)}
}

// Diff compares two commits of one root: the newest clean snapshot of each (or the explicit
// overrides), their changed documents, symbols classified by identity, and with Stat the line counts
// of every symbol from hash-verified Git blobs.
func Diff(ctx context.Context, database *gorm.DB, options Options) (Result, error) {
	visibility, err := ParseVisibility(string(options.Visibility))
	if err != nil {
		return Result{}, err
	}
	if options.From == "" || options.To == "" {
		return Result{}, fmt.Errorf("commit range %q..%q has an empty revision", options.From, options.To)
	}
	if database == nil {
		return Result{}, errors.New("diff: database is required")
	}
	scope, err := loadRootScope(ctx, database, options.RootKey)
	if err != nil {
		return Result{}, err
	}
	before, after, err := selectSides(ctx, database, scope, options)
	if err != nil {
		return Result{}, err
	}
	activeBefore, err := storage.ActiveDocuments(ctx, database, before.snapshot.ID)
	if err != nil {
		return Result{}, err
	}
	activeAfter, err := storage.ActiveDocuments(ctx, database, after.snapshot.ID)
	if err != nil {
		return Result{}, err
	}
	files, err := changedFiles(activeBefore, activeAfter)
	if err != nil {
		return Result{}, err
	}
	pairings := matchSymbols(files)
	result := Result{RootKey: scope.root.RootKey, From: before.info(), To: after.info(), Visibility: visibility, Stat: options.Stat, Packages: []PackageDiff{}}
	var attribution *lineAttribution
	var manifests []FileDiff
	if options.Stat {
		if attribution, manifests, err = statLines(ctx, scope, files, pairings, before, after, &result); err != nil {
			return Result{}, err
		}
	}
	markers := coverageMarkers{before: activeBefore, after: activeAfter}
	rows, err := buildRows(pairings, attribution, markers, visibility)
	if err != nil {
		return Result{}, err
	}
	result.Packages = assemble(files, rows, attribution, markers, manifests, scope.root.RootKey)
	return result, nil
}

func selectSides(ctx context.Context, database *gorm.DB, scope rootScope, options Options) (selectedSide, selectedSide, error) {
	var sides [2]selectedSide
	for index, request := range []struct{ revision, override, flag string }{
		{options.From, options.SnapshotFrom, "--snapshot-from"}, {options.To, options.SnapshotTo, "--snapshot-to"},
	} {
		commit, err := scope.resolveCommit(ctx, request.revision)
		if err != nil {
			return selectedSide{}, selectedSide{}, err
		}
		snapshot, err := scope.selectSnapshot(ctx, database, commit, request.override, request.flag)
		if err != nil {
			return selectedSide{}, selectedSide{}, err
		}
		sides[index] = selectedSide{commit: commit, snapshot: snapshot}
	}
	if sides[0].snapshot.ConfigurationHash != sides[1].snapshot.ConfigurationHash {
		return selectedSide{}, selectedSide{}, fmt.Errorf("snapshots %s and %s were indexed under different configuration hashes (%s, %s), so their shape and body hashes are not comparable",
			sides[0].snapshot.ID, sides[1].snapshot.ID, sides[0].snapshot.ConfigurationHash, sides[1].snapshot.ConfigurationHash)
	}
	return sides[0], sides[1], nil
}

// statLines locates both commits' checkouts, attributes lines, and compares the manifests. A missing
// checkout fails every file's line counts, and the result says why.
func statLines(ctx context.Context, scope rootScope, files []changedFile, pairings []*pairing, before, after selectedSide, result *Result) (*lineAttribution, []FileDiff, error) {
	sources := [2]blobSource{newBlobSource(ctx, scope, before.commit), newBlobSource(ctx, scope, after.commit)}
	result.From.Checkout, result.To.Checkout = sources[0].checkout, sources[1].checkout
	attribution := attributeLines(ctx, files, pairings, sources[0], sources[1])
	if err := errors.Join(sources[0].err, sources[1].err); err != nil {
		result.LinesError = err.Error()
		return &attribution, nil, nil
	}
	manifests, err := compareManifests(ctx, sources, [2]storage.ModuleSnapshot{before.snapshot, after.snapshot})
	return &attribution, manifests, err
}

// coverageMarkers reads the coverage of each side's active document at a path.
type coverageMarkers struct {
	before, after map[string]storage.ActiveDocument
}

func (markers coverageMarkers) of(paths ...string) []string {
	found := map[storage.Coverage]bool{}
	for _, path := range paths {
		for _, active := range []map[string]storage.ActiveDocument{markers.before, markers.after} {
			if document, ok := active[path]; ok {
				found[document.Document.Coverage] = true
			}
		}
	}
	var result []string
	for _, coverage := range []storage.Coverage{storage.CoveragePartial, storage.CoverageSyntax, storage.CoverageExcluded} {
		if found[coverage] {
			result = append(result, string(coverage))
		}
	}
	return result
}

func (markers coverageMarkers) ofPairing(pair *pairing) []string {
	var paths []string
	for _, side := range []*entry{pair.before, pair.after} {
		if side != nil {
			paths = append(paths, side.file.path)
		}
	}
	return markers.of(paths...)
}

// reportedRow is a row with the pairing it describes, the file it is reported under, and whether the
// visibility filter keeps it.
type reportedRow struct {
	row     Row
	pair    *pairing
	path    string
	pkg     string
	visible bool
}

var classOrder = map[Class]int{ClassRemoved: 0, ClassAdded: 1, ClassSignature: 2, ClassBody: 3, ClassMoved: 4}

// buildRows turns changed pairings into rows. With line counts, an unchanged pairing whose lines
// differ (comments or layout inside the extent) is a body row saying so.
func buildRows(pairings []*pairing, attribution *lineAttribution, markers coverageMarkers, visibility Visibility) ([]reportedRow, error) {
	var rows []reportedRow
	for _, pair := range pairings {
		class, note := pair.class, ""
		var count *LineCount
		if attribution != nil {
			if lines, ok := attribution.pairs[pair]; ok {
				count = &lines
			}
		}
		if class == "" {
			if count == nil || *count == (LineCount{}) {
				continue
			}
			class, note = ClassBody, NoteLayoutOnly
		}
		row, err := newRow(pair, class, markers.ofPairing(pair))
		if err != nil {
			return nil, err
		}
		row.Lines, row.Note = count, note
		reported := pair.reported()
		rows = append(rows, reportedRow{row: row, pair: pair, path: reported.file.path, pkg: reported.file.active.Source.PackagePath, visible: pair.visibleAs(visibility)})
	}
	groupRetypedPairs(rows)
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if left.path != right.path {
			return left.path < right.path
		}
		if left.row.DisplayName() != right.row.DisplayName() {
			return left.row.DisplayName() < right.row.DisplayName()
		}
		return classOrder[left.row.Class] < classOrder[right.row.Class]
	})
	return rows, nil
}

// groupRetypedPairs groups a parameter-type change: exactly one removed and one added canonical
// symbol of the same package, kind, owner, and name. The added row carries the old shape and the
// shape diff, so the change reads as one signature edit.
func groupRetypedPairs(rows []reportedRow) {
	type groupKey struct{ pkg, kind, owner, name string }
	removed, added := map[groupKey][]int{}, map[groupKey][]int{}
	for index, row := range rows {
		key := groupKey{row.pkg, row.row.Kind, row.row.Owner, row.row.Name}
		switch {
		case row.row.Class == ClassRemoved && row.pair.before.symbol.ID != nil:
			removed[key] = append(removed[key], index)
		case row.row.Class == ClassAdded && row.pair.after.symbol.ID != nil:
			added[key] = append(added[key], index)
		}
	}
	for key, removedRows := range removed {
		if len(removedRows) != 1 || len(added[key]) != 1 {
			continue
		}
		old, current := &rows[removedRows[0]].row, &rows[added[key][0]].row
		old.Group, current.Group = old.DisplayName(), old.DisplayName()
		current.ShapeBefore = old.ShapeBefore
		current.ShapeDiff = DiffShapes(old.ShapeBefore, current.ShapeAfter)
	}
}
