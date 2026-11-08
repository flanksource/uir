package testruns

import (
	"context"
	"maps"
	"slices"
	"strconv"

	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// packageFingerprint covers every declaration in the test's package and external test package. It is
// the conservative setup/shared-state guard for init, TestMain and package globals.
func packageFingerprint(ctx context.Context, database *gorm.DB, snapshot uuid.UUID, packagePath string) (string, bool, error) {
	var handles []int64
	if err := database.WithContext(ctx).Model(&storage.Symbol{}).
		Where("package_path IN ?", []string{packagePath, packagePath + "_test"}).Pluck("handle", &handles).Error; err != nil {
		return "", false, err
	}
	handles, err := storage.DefinedSymbols(ctx, database, snapshot, handles)
	if err != nil || len(handles) == 0 {
		return "", false, err
	}
	generations, err := storage.SymbolGenerations(ctx, database, snapshot, handles)
	if err != nil {
		return "", false, err
	}
	ids, err := storage.SymbolIDs(ctx, database, handles)
	if err != nil {
		return "", false, err
	}
	pairs := map[string]int64{}
	for handle, id := range ids {
		generation, found := generations[handle]
		if !found || generation.ContentFP == nil {
			return "", false, nil
		}
		pairs[id] = *generation.ContentFP
	}
	digest := canonicalhash.New("uir-test-package-v1")
	digest.Count(len(pairs))
	for _, id := range slices.Sorted(maps.Keys(pairs)) {
		digest.Text(id)
		digest.Text(strconv.FormatInt(pairs[id], 10))
	}
	return digest.Sum(), true, nil
}
