package storage

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir/storage/symbolhandle"
	"gorm.io/gorm"
)

// packageKey is a package path within a numbered module.
type packageKey struct {
	module uint64
	path   string
}

// handleRegistry is the module and package numbering of one allocation; created lists the packages
// this allocation numbered, whose buckets are empty.
type handleRegistry struct {
	modules  map[string]uint64
	packages map[packageKey]uint64
	created  map[packageKey]bool
}

// AssignSymbolHandles sets Handle on every row. A symbol already stored keeps its stored handle, which
// must agree with the row's module, package, visibility, and kind. A new symbol gets the next local of
// its (module, package, visibility, kind) bucket: 0 in a package this call registered, otherwise one
// past the bucket's largest stored handle, found by a range scan of symbols_handle_key. Rows are
// allocated in the given order and missing module and package numbers in key order, so the same
// publications in the same order produce the same handles. Call it inside the publication
// transaction; a concurrent publisher taking the same numbers surfaces as ErrAllocationConflict when
// the rows are written.
func AssignSymbolHandles(ctx context.Context, database *gorm.DB, rows []Symbol) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	if duplicate := firstDuplicate(ids); duplicate != "" {
		return fmt.Errorf("assign symbol handles: symbol %s appears twice", duplicate)
	}
	stored, err := loadSurrogates[string, int64](ctx, database, Symbol{}.TableName(), "id", "handle", "symbol id", ids)
	if err != nil {
		return err
	}
	registry, err := resolveHandleRegistry(ctx, database, rows)
	if err != nil {
		return err
	}
	next := map[int64]int64{}
	for i := range rows {
		fields, bucket, err := registry.bucketOf(rows[i])
		if err != nil {
			return err
		}
		if handle, found := stored[rows[i].ID]; found {
			if err := checkStoredHandle(rows[i], handle, fields); err != nil {
				return err
			}
			rows[i].Handle = handle
			continue
		}
		handle, known := next[bucket.Low]
		if !known {
			if handle, err = registry.firstFree(ctx, database, bucket, packageKey{fields.Module, rows[i].PackagePath}); err != nil {
				return err
			}
		}
		fields.Local = uint64(handle - bucket.Low)
		if rows[i].Handle, err = symbolhandle.Pack(fields); err != nil {
			return fmt.Errorf("allocate a handle for symbol %s (%s): %w", rows[i].ID, rows[i].CanonicalKey, err)
		}
		next[bucket.Low] = handle + 1
	}
	return nil
}

func firstDuplicate(ids []string) string {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return id
		}
		seen[id] = true
	}
	return ""
}

func checkStoredHandle(row Symbol, handle int64, expected symbolhandle.Fields) error {
	fields, err := symbolhandle.Unpack(handle)
	if err != nil {
		return fmt.Errorf("symbol %s: %w", row.ID, err)
	}
	fields.Local = 0
	if fields != expected {
		return fmt.Errorf("symbol %s (%s) has stored handle %d with %+v, which disagrees with its module, package, visibility, and kind %+v",
			row.ID, row.CanonicalKey, handle, fields, expected)
	}
	return nil
}

func (registry handleRegistry) bucketOf(row Symbol) (symbolhandle.Fields, symbolhandle.Range, error) {
	kind, err := symbolhandle.ParseKind(row.Kind)
	if err != nil {
		return symbolhandle.Fields{}, symbolhandle.Range{}, fmt.Errorf("symbol %s: %w", row.ID, err)
	}
	visibility, err := symbolhandle.ParseVisibility(row.Visibility)
	if err != nil {
		return symbolhandle.Fields{}, symbolhandle.Range{}, fmt.Errorf("symbol %s: %w", row.ID, err)
	}
	module := registry.modules[row.ModuleKey]
	fields := symbolhandle.Fields{Module: module, Package: registry.packages[packageKey{module, row.PackagePath}], Visibility: visibility, Kind: kind}
	bucket, err := symbolhandle.BucketRange(fields.Module, fields.Package, visibility, kind)
	return fields, bucket, err
}

// firstFree is the first unused handle of a bucket.
func (registry handleRegistry) firstFree(ctx context.Context, database *gorm.DB, bucket symbolhandle.Range, pkg packageKey) (int64, error) {
	if registry.created[pkg] {
		return bucket.Low, nil
	}
	var largest []int64
	if err := database.WithContext(ctx).Model(&Symbol{}).Where("handle BETWEEN ? AND ?", bucket.Low, bucket.High).
		Order("handle DESC").Limit(1).Pluck("handle", &largest).Error; err != nil {
		return 0, fmt.Errorf("read the largest handle in [%d, %d]: %w", bucket.Low, bucket.High, err)
	}
	if len(largest) == 0 {
		return bucket.Low, nil
	}
	return largest[0] + 1, nil
}

func resolveHandleRegistry(ctx context.Context, database *gorm.DB, rows []Symbol) (handleRegistry, error) {
	registry := handleRegistry{modules: map[string]uint64{}, packages: map[packageKey]uint64{}, created: map[packageKey]bool{}}
	moduleKeys := map[string]bool{}
	for _, row := range rows {
		moduleKeys[row.ModuleKey] = true
	}
	if err := registry.resolveModules(ctx, database, sortedSet(moduleKeys)); err != nil {
		return handleRegistry{}, err
	}
	paths := map[uint64]map[string]bool{}
	for _, row := range rows {
		module := registry.modules[row.ModuleKey]
		if paths[module] == nil {
			paths[module] = map[string]bool{}
		}
		paths[module][row.PackagePath] = true
	}
	modules := make([]uint64, 0, len(paths))
	for module := range paths {
		modules = append(modules, module)
	}
	slices.Sort(modules)
	for _, module := range modules {
		if err := registry.resolvePackages(ctx, database, module, sortedSet(paths[module])); err != nil {
			return handleRegistry{}, err
		}
	}
	return registry, nil
}

func sortedSet[K cmp.Ordered](set map[K]bool) []K {
	keys := make([]K, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// resolveModules loads the numbers of moduleKeys and registers the missing ones: a reserved key under
// its fixed number, any other from one past the largest allocated number.
func (registry handleRegistry) resolveModules(ctx context.Context, database *gorm.DB, moduleKeys []string) error {
	var stored []SymbolModule
	if err := database.WithContext(ctx).Where("module_key IN ?", moduleKeys).Find(&stored).Error; err != nil {
		return fmt.Errorf("load symbol modules: %w", err)
	}
	for _, row := range stored {
		registry.modules[row.ModuleKey] = uint64(row.Number)
	}
	var missing []SymbolModule
	next := int64(-1)
	for _, key := range moduleKeys {
		if _, found := registry.modules[key]; found {
			continue
		}
		number, reserved := symbolhandle.ReservedModuleNumber(key)
		if !reserved {
			if next < 0 {
				largest, err := largestNumber(ctx, database.Model(&SymbolModule{}), "symbol module")
				if err != nil {
					return err
				}
				next = max(largest+1, int64(symbolhandle.FirstAllocatedModule))
			}
			number, next = uint64(next), next+1
		}
		if number > symbolhandle.MaxModule {
			return fmt.Errorf("symbol handle module %d for module %q exceeds %d bits", number, key, symbolhandle.ModuleBits)
		}
		registry.modules[key] = number
		missing = append(missing, SymbolModule{Number: int32(number), ModuleKey: key})
	}
	if len(missing) == 0 {
		return nil
	}
	if err := database.WithContext(ctx).Create(&missing).Error; err != nil {
		return allocationConflict(err, fmt.Sprintf("register %d symbol modules", len(missing)))
	}
	return nil
}

// resolvePackages loads the numbers of one module's package paths and registers the missing ones from
// one past the module's largest package number.
func (registry handleRegistry) resolvePackages(ctx context.Context, database *gorm.DB, module uint64, paths []string) error {
	for start := 0; start < len(paths); start += surrogateLookupBatch {
		var stored []SymbolPackage
		if err := database.WithContext(ctx).Where("module_number = ? AND package_path IN ?", module, paths[start:min(start+surrogateLookupBatch, len(paths))]).
			Find(&stored).Error; err != nil {
			return fmt.Errorf("load symbol packages of module %d: %w", module, err)
		}
		for _, row := range stored {
			registry.packages[packageKey{module, row.PackagePath}] = uint64(row.Number)
		}
	}
	var missing []SymbolPackage
	next := int64(-1)
	for _, path := range paths {
		key := packageKey{module, path}
		if _, found := registry.packages[key]; found {
			continue
		}
		if next < 0 {
			largest, err := largestNumber(ctx, database.Model(&SymbolPackage{}).Where("module_number = ?", module), "symbol package")
			if err != nil {
				return err
			}
			next = largest + 1
		}
		if uint64(next) > symbolhandle.MaxPackage {
			return fmt.Errorf("symbol handle package %d for %q in module %d exceeds %d bits", next, path, module, symbolhandle.PackageBits)
		}
		registry.packages[key], registry.created[key] = uint64(next), true
		missing = append(missing, SymbolPackage{ModuleNumber: int32(module), Number: int32(next), PackagePath: path})
		next++
	}
	if len(missing) == 0 {
		return nil
	}
	if err := database.WithContext(ctx).CreateInBatches(missing, surrogateLookupBatch).Error; err != nil {
		return allocationConflict(err, fmt.Sprintf("register %d symbol packages of module %d", len(missing), module))
	}
	return nil
}

// largestNumber is the largest registry number the scoped query selects, or -1 when it selects none.
func largestNumber(ctx context.Context, scoped *gorm.DB, subject string) (int64, error) {
	var largest []int64
	if err := scoped.WithContext(ctx).Order("number DESC").Limit(1).Pluck("number", &largest).Error; err != nil {
		return 0, fmt.Errorf("read the largest %s number: %w", subject, err)
	}
	if len(largest) == 0 {
		return -1, nil
	}
	return largest[0], nil
}
