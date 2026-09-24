package symboldiff

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/uir/storage"
)

// manifestNames are the dependency manifests a module's content set hash covers under its own root.
var manifestNames = []string{"go.mod", "go.sum"}

// assemble groups rows under their files and files under their packages, filters by visibility
// while counting hidden rows, and totals line counts. A file whose bytes are equal on both sides and
// that reports no row is omitted.
func assemble(files []changedFile, rows []reportedRow, attribution *lineAttribution, markers coverageMarkers, manifests []FileDiff, rootKey string) []PackageDiff {
	byPath := map[string]*FileDiff{}
	packageOf := map[string]string{}
	rowsOf := map[string][]reportedRow{}
	for _, row := range rows {
		rowsOf[row.path] = append(rowsOf[row.path], row)
	}
	packages := map[string]*PackageDiff{}
	for _, file := range files {
		diff := newFileDiff(file, markers)
		for _, row := range rowsOf[file.path] {
			if row.visible {
				diff.Rows = append(diff.Rows, row.row)
			} else {
				diff.HiddenRows++
			}
		}
		if file.before != nil && file.after != nil && file.before.active.Source.ContentHash == file.after.active.Source.ContentHash &&
			len(diff.Rows) == 0 && diff.HiddenRows == 0 {
			continue
		}
		if attribution != nil {
			totalFile(&diff, rowsOf[file.path], *attribution)
		}
		byPath[file.path] = &diff
		packageOf[file.path] = file.reported().active.Source.PackagePath
	}
	for _, manifest := range manifests {
		byPath[manifest.Path] = &manifest
		packageOf[manifest.Path] = rootKey
	}
	for path, file := range byPath {
		pkg := packages[packageOf[path]]
		if pkg == nil {
			pkg = &PackageDiff{Path: packageOf[path]}
			packages[packageOf[path]] = pkg
		}
		pkg.Files = append(pkg.Files, *file)
	}
	return sortedPackages(packages, attribution != nil)
}

func (file changedFile) reported() *fileSide {
	if file.after != nil {
		return file.after
	}
	return file.before
}

func newFileDiff(file changedFile, markers coverageMarkers) FileDiff {
	diff := FileDiff{Path: file.path, Status: FileModified, Coverage: markers.of(file.path), Rows: []Row{}}
	switch {
	case file.before == nil:
		diff.Status = FileAdded
	case file.after == nil:
		diff.Status = FileRemoved
	}
	for _, side := range []*fileSide{file.after, file.before} {
		if side != nil && side.content.Excluded != "" && diff.Excluded == "" {
			diff.Excluded = side.content.Excluded
		}
	}
	return diff
}

// totalFile sums every row reported under the file, hidden ones included, plus its file scope. Any
// row whose segments could not be read fails the file's counts with the reason.
func totalFile(diff *FileDiff, rows []reportedRow, attribution lineAttribution) {
	if reason := attribution.failures[diff.Path]; reason != "" {
		diff.LinesError = reason
		return
	}
	scope := attribution.fileScope[diff.Path]
	total := scope
	for _, row := range rows {
		if reason := attribution.pairFailures[row.pair]; reason != "" {
			diff.LinesError = reason
			return
		}
		if row.row.Lines != nil {
			total.add(*row.row.Lines)
		}
	}
	diff.FileScope, diff.Lines = &scope, &total
}

func sortedPackages(packages map[string]*PackageDiff, stat bool) []PackageDiff {
	result := make([]PackageDiff, 0, len(packages))
	for _, pkg := range packages {
		sort.Slice(pkg.Files, func(i, j int) bool { return pkg.Files[i].Path < pkg.Files[j].Path })
		if stat {
			total := LineCount{}
			complete := true
			for _, file := range pkg.Files {
				if file.Lines == nil {
					complete = false
					break
				}
				total.add(*file.Lines)
			}
			if complete {
				pkg.Lines = &total
			}
		}
		result = append(result, *pkg)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

// compareManifests lists go.mod and go.sum when their blobs differ between the commits, with their
// whole line diff as file scope. A dirty snapshot's manifest bytes are not the commit's, so a dirty
// side fails the manifest's counts instead.
func compareManifests(ctx context.Context, sources [2]blobSource, snapshots [2]storage.ModuleSnapshot) ([]FileDiff, error) {
	var manifests []FileDiff
	for _, name := range manifestNames {
		var blobs [2]string
		for index, source := range sources {
			output, err := git(ctx, source.checkout, "ls-tree", source.commit, "--", name)
			if err != nil {
				return nil, fmt.Errorf("list manifest %s at %s: %w", name, source.commit, err)
			}
			if fields := strings.Fields(string(output)); len(fields) >= 3 {
				blobs[index] = fields[2]
			}
		}
		if blobs[0] == blobs[1] {
			continue
		}
		manifest, err := diffManifest(ctx, name, sources, blobs, snapshots)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

func diffManifest(ctx context.Context, name string, sources [2]blobSource, blobs [2]string, snapshots [2]storage.ModuleSnapshot) (FileDiff, error) {
	manifest := FileDiff{Path: name, Status: FileModified, Coverage: []string{MarkerManifest}, Rows: []Row{}}
	switch {
	case blobs[0] == "":
		manifest.Status = FileAdded
	case blobs[1] == "":
		manifest.Status = FileRemoved
	}
	var contents [2]string
	for index, blob := range blobs {
		if blob == "" {
			continue
		}
		if snapshots[index].WorktreeState != storage.WorktreeClean {
			manifest.LinesError = fmt.Sprintf("snapshot %s is %s, so %s at %s cannot be verified against it", snapshots[index].ID, snapshots[index].WorktreeState, name, sources[index].commit)
			return manifest, nil
		}
		content, err := sources[index].checkoutBytes(ctx, name)
		if err != nil {
			return FileDiff{}, fmt.Errorf("read manifest %s at %s: %w", name, sources[index].commit, err)
		}
		contents[index] = string(content)
	}
	count := countLines(contents[0], contents[1])
	manifest.FileScope, manifest.Lines = &count, &LineCount{Added: count.Added, Removed: count.Removed}
	return manifest, nil
}
