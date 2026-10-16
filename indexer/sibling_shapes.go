package indexer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// headCheck decides, for one index call, whether heads are reusable without type-checking. It knows
// the current export shapes of the module locations the call or its run has published or confirmed,
// which are the only shapes a head's recorded workspace sibling shapes are compared against.
type headCheck struct {
	database     *gorm.DB
	memo         *runMemo
	includeTests bool
	// resolveVersions links roots' versioned dependencies to the snapshots the run resolves them to.
	resolveVersions func(ctx context.Context, roots []discoveredRoot, includeTests bool) error
	confirmed       map[string]confirmedLocation
	// siblings caches siblingLocations by root location.
	siblings map[string]map[string]string
	// checking guards the locations whose heads are being confirmed against a cycle.
	checking map[string]bool
	// workFiles caches the go.work each sibling location builds under.
	workFiles map[string]string
}

// confirmedLocation is a location's current package shapes, by package path, and its current snapshot,
// which is nil for a root this call extracted and has not published yet.
type confirmedLocation struct {
	snapshot *storage.ModuleSnapshot
	shapes   map[string]*string
}

// headMiss is why a head is not reusable; the zero value means it is. counter names a miss the run
// counts: one about the head's workspace siblings.
type headMiss struct {
	counter, detail string
}

func newHeadCheck(indexer *Indexer, memo *runMemo, includeTests bool) *headCheck {
	return &headCheck{
		database: indexer.database, memo: memo, includeTests: includeTests, resolveVersions: indexer.indexVersionedDependencies,
		confirmed: map[string]confirmedLocation{}, siblings: map[string]map[string]string{}, checking: map[string]bool{},
		workFiles: map[string]string{},
	}
}

// confirmExtraction records the shapes a root extracted in this call will publish.
func (check *headCheck) confirmExtraction(extraction moduleExtraction) {
	shapes := make(map[string]*string, len(extraction.packages))
	for _, extracted := range extraction.packages {
		shapes[extracted.path] = extracted.exportShapeHash
	}
	check.confirmed[extraction.root.LocalPath] = confirmedLocation{shapes: shapes}
}

// confirmSnapshot records a snapshot this call confirmed as the location's current one.
func (check *headCheck) confirmSnapshot(ctx context.Context, location string, id uuid.UUID) (confirmedLocation, error) {
	var snapshot storage.ModuleSnapshot
	if err := check.database.WithContext(ctx).Where("id = ?", id).Take(&snapshot).Error; err != nil {
		return confirmedLocation{}, fmt.Errorf("load snapshot %s of %q: %w", id, location, err)
	}
	var rows []storage.PackageCoverage
	if err := check.database.WithContext(ctx).Where("snapshot_id = ?", id).Find(&rows).Error; err != nil {
		return confirmedLocation{}, fmt.Errorf("load package shapes of snapshot %s: %w", id, err)
	}
	confirmed := confirmedLocation{snapshot: &snapshot, shapes: make(map[string]*string, len(rows))}
	for _, row := range rows {
		confirmed.shapes[row.PackagePath] = row.ExportShapeHash
	}
	check.confirmed[location] = confirmed
	return confirmed, nil
}

// siblingsOf is siblingLocations of the root, read once per check.
func (check *headCheck) siblingsOf(root discoveredRoot) (map[string]string, error) {
	if cached, found := check.siblings[root.LocalPath]; found {
		return cached, nil
	}
	locations, err := siblingLocations(root)
	if err == nil {
		check.siblings[root.LocalPath] = locations
	}
	return locations, err
}

// siblingLocations maps each workspace sibling import of the root to its module's canonical location.
// A module the root depends on through a local directory is left out: the dependency set hash already
// names its snapshot.
func siblingLocations(root discoveredRoot) (map[string]string, error) {
	imports, directories, err := workspaceSiblingImports(root)
	if err != nil {
		return nil, err
	}
	covered := map[string]bool{}
	for _, dependency := range root.Dependencies {
		covered[dependency.Edge.ModulePath] = dependency.LocalDir != ""
	}
	locations := map[string]string{}
	for path, module := range imports {
		if covered[module] {
			continue
		}
		location, err := filepath.EvalSymlinks(directories[module])
		if err != nil {
			return nil, fmt.Errorf("resolve workspace module %q imported by %q: %w", module, root.RootKey, err)
		}
		locations[path] = location
	}
	return locations, nil
}

// siblingsCurrent is why the head's recorded siblings do not hold: it recorded none although the root
// imports siblings, or a sibling indexed under the root's go.work is unconfirmed or presents another
// shape now. A sibling indexed under another go.work has shapes computed against another build list,
// so it is confirmed by sources instead: every recorded package's source digest must be unchanged.
func (check *headCheck) siblingsCurrent(ctx context.Context, root discoveredRoot, head uuid.UUID, siblings map[string]string) (headMiss, error) {
	if len(siblings) == 0 {
		return headMiss{}, nil
	}
	var recorded []storage.SnapshotSiblingShape
	if err := check.database.WithContext(ctx).Where("snapshot_id = ?", head).Order("import_path").Find(&recorded).Error; err != nil {
		return headMiss{}, fmt.Errorf("load sibling shapes of snapshot %s: %w", head, err)
	}
	if len(recorded) == 0 {
		return headMiss{counter: "sibling-unrecorded", detail: fmt.Sprintf("head %s recorded no sibling shapes", head)}, nil
	}
	bySources := false
	for _, row := range recorded {
		location, imported := siblings[row.ImportPath]
		if !imported || row.ExportShapeHash == nil {
			continue
		}
		workFile, err := check.workFile(ctx, location)
		if err != nil {
			return headMiss{}, err
		}
		if workFile != root.WorkFile {
			if row.SourceDigest == nil {
				return headMiss{counter: "sibling-unrecorded", detail: fmt.Sprintf("%s at %s is indexed under go.work %q, not %q, and head %s recorded no source digests",
					row.ImportPath, location, workFile, root.WorkFile, head)}, nil
			}
			bySources = true
			continue
		}
		if miss, err := check.shapeCurrent(ctx, row, location); err != nil || miss != (headMiss{}) {
			return miss, err
		}
	}
	if !bySources {
		return headMiss{}, nil
	}
	return check.sourcesCurrent(root, recorded)
}

// shapeCurrent is why a sibling indexed under the root's go.work does not present its recorded shape.
func (check *headCheck) shapeCurrent(ctx context.Context, row storage.SnapshotSiblingShape, location string) (headMiss, error) {
	confirmed, reason, err := check.confirmLocation(ctx, location)
	if err != nil {
		return headMiss{}, err
	}
	if reason != "" {
		return headMiss{counter: "sibling-unconfirmed", detail: fmt.Sprintf("%s at %s: %s", row.ImportPath, location, reason)}, nil
	}
	if shape := confirmed.shapes[row.ImportPath]; shape == nil || *shape != *row.ExportShapeHash {
		return headMiss{counter: "sibling-changed", detail: fmt.Sprintf("%s at %s presents another export shape", row.ImportPath, location)}, nil
	}
	return headMiss{}, nil
}

// sourcesCurrent is which recorded sibling package's sources differ on disk from what the head's load
// read, or no longer come from a module the root's build loads from disk.
func (check *headCheck) sourcesCurrent(root discoveredRoot, recorded []storage.SnapshotSiblingShape) (headMiss, error) {
	modules, err := diskModules(root)
	if err != nil {
		return headMiss{}, err
	}
	for _, row := range recorded {
		if row.SourceDigest == nil {
			continue
		}
		module := owningModule(modules, row.ImportPath)
		if module == "" || module == root.RootKey {
			return headMiss{counter: "sibling-source-changed", detail: fmt.Sprintf("%s is no longer loaded from a workspace module", row.ImportPath)}, nil
		}
		moduleDir, err := filepath.EvalSymlinks(modules[module])
		if err != nil {
			return headMiss{}, fmt.Errorf("resolve workspace module %q: %w", module, err)
		}
		packageDir := filepath.Join(moduleDir, filepath.FromSlash(strings.TrimPrefix(row.ImportPath, module)))
		digest, err := packageSourceDigest(moduleDir, packageDir, nil)
		if err != nil {
			return headMiss{}, err
		}
		if digest != *row.SourceDigest {
			return headMiss{counter: "sibling-source-changed", detail: fmt.Sprintf("%s at %s: sources differ from what head's load read", row.ImportPath, packageDir)}, nil
		}
	}
	return headMiss{}, nil
}

// workFile is the go.work the location builds under, read once per check.
func (check *headCheck) workFile(ctx context.Context, location string) (string, error) {
	if workFile, found := check.workFiles[location]; found {
		return workFile, nil
	}
	environment, err := readGoEnvironment(ctx, location)
	if err != nil {
		return "", err
	}
	check.workFiles[location] = environment.WorkFile
	return environment.WorkFile, nil
}

// confirmLocation is the location's current snapshot and shapes, when this call or its run published
// or confirmed it, or its head is reusable itself; otherwise it says why not.
func (check *headCheck) confirmLocation(ctx context.Context, location string) (confirmedLocation, string, error) {
	if confirmed, found := check.confirmed[location]; found {
		return confirmed, "", nil
	}
	if result, found := check.memo.module(location, check.includeTests); found {
		id, err := uuid.Parse(result.SnapshotID)
		if err != nil {
			return confirmedLocation{}, "", fmt.Errorf("remembered snapshot %q of %q: %w", result.SnapshotID, location, err)
		}
		confirmed, err := check.confirmSnapshot(ctx, location, id)
		return confirmed, "", err
	}
	return check.reusedLocation(ctx, location)
}

// reusedLocation confirms a location the run has not indexed by checking its own head.
func (check *headCheck) reusedLocation(ctx context.Context, location string) (confirmedLocation, string, error) {
	root, err := discoverModule(ctx, location, location, check.includeTests)
	if err != nil {
		return confirmedLocation{}, "", fmt.Errorf("discover %q: %w", location, err)
	}
	return check.reusedRoot(ctx, root)
}

// reusedRoot confirms a discovered root by checking its own head, after linking its dependencies to the
// snapshots the run resolved or confirmed for them; otherwise it says why not.
func (check *headCheck) reusedRoot(ctx context.Context, root discoveredRoot) (confirmedLocation, string, error) {
	if check.checking[root.LocalPath] {
		return confirmedLocation{}, "its dependencies or siblings lead back to it", nil
	}
	check.checking[root.LocalPath] = true
	defer delete(check.checking, root.LocalPath)
	var err error
	if root.Dependencies, err = check.memo.dependencies(ctx, root); err != nil {
		return confirmedLocation{}, "", fmt.Errorf("read dependencies of %q: %w", root.LocalPath, err)
	}
	reason, err := check.linkConfirmed(ctx, &root)
	if err != nil || reason != "" {
		return confirmedLocation{}, reason, err
	}
	head, miss, err := check.headReuse(ctx, root)
	if err != nil || miss != (headMiss{}) {
		return confirmedLocation{}, "head not reusable: " + miss.detail, err
	}
	confirmed, err := check.confirmSnapshot(ctx, root.LocalPath, head)
	return confirmed, "", err
}

// linkConfirmed links every dependency of root as IndexModules would before extracting: a local one to
// its location's confirmed snapshot, a versioned one to the snapshot the run resolves it to, sharing
// that resolution with the rest of the run. It says which local dependency it cannot link.
func (check *headCheck) linkConfirmed(ctx context.Context, root *discoveredRoot) (string, error) {
	for index := range root.Dependencies {
		dependency := &root.Dependencies[index]
		if dependency.LocalDir == "" {
			continue
		}
		if reason, err := check.linkLocal(ctx, root, dependency); err != nil || reason != "" {
			return reason, err
		}
	}
	roots := []discoveredRoot{*root}
	if err := check.resolveVersions(ctx, roots, check.includeTests); err != nil {
		return "", fmt.Errorf("resolve versioned dependencies of %q: %w", root.LocalPath, err)
	}
	*root = roots[0]
	return "", nil
}

// linkLocal links a local dependency to its location's confirmed snapshot, as indexLocalDependencies
// links it to the snapshot it indexes.
func (check *headCheck) linkLocal(ctx context.Context, root *discoveredRoot, dependency *dependencyObservation) (string, error) {
	path, err := filepath.EvalSymlinks(dependency.LocalDir)
	if err != nil {
		dependency.Edge.UnresolvedReason = fmt.Sprintf("resolve local dependency %q: %v", dependency.LocalDir, err)
		return "", nil
	}
	dependency.LocalDir = path
	target, reason, err := check.confirmLocation(ctx, path)
	if err != nil || reason != "" {
		return fmt.Sprintf("local dependency %s: %s", path, reason), err
	}
	if target.snapshot == nil {
		return fmt.Sprintf("local dependency %s is extracted but not yet published", path), nil
	}
	linkLocalSnapshot(root, dependency, *target.snapshot)
	return "", nil
}
