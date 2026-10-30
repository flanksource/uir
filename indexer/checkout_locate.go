package indexer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"gorm.io/gorm"
)

// Checkout is a registered checkout: the module path it is registered under and its canonical directory.
type Checkout struct {
	RootKey  string
	Location string
}

// LocateCheckout finds the registered checkout of the Go module enclosing directory: the nearest go.mod
// at or above it, matched by module path and canonical directory. A directory outside any module, or in
// a module not registered at that directory, is not found.
func LocateCheckout(ctx context.Context, database *gorm.DB, directory string) (Checkout, bool, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return Checkout{}, false, fmt.Errorf("resolve directory %q: %w", directory, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Checkout{}, false, fmt.Errorf("canonicalize directory %q: %w", absolute, err)
	}
	moduleDir, rootKey, found, err := enclosingModule(canonical)
	if err != nil || !found {
		return Checkout{}, false, err
	}
	var location string
	err = database.WithContext(ctx).Table("locations AS location").
		Select("location.canonical_path").Joins("JOIN modules AS root ON root.id = location.root_id").
		Where("root.root_key = ? AND location.canonical_path = ?", rootKey, moduleDir).
		Limit(1).Scan(&location).Error
	if err != nil {
		return Checkout{}, false, fmt.Errorf("load registered checkout of %q at %q: %w", rootKey, moduleDir, err)
	}
	if location == "" {
		return Checkout{}, false, nil
	}
	return Checkout{RootKey: rootKey, Location: location}, true, nil
}

// enclosingModule walks up from directory to the nearest go.mod and returns its directory and module path.
func enclosingModule(directory string) (string, string, bool, error) {
	for current := directory; ; current = filepath.Dir(current) {
		modPath := filepath.Join(current, "go.mod")
		content, err := os.ReadFile(modPath)
		if err == nil {
			module, parseErr := modfile.Parse(modPath, content, nil)
			if parseErr != nil {
				return "", "", false, fmt.Errorf("parse %s: %w", modPath, parseErr)
			}
			if module.Module == nil || strings.TrimSpace(module.Module.Mod.Path) == "" {
				return "", "", false, fmt.Errorf("%s has no module path", modPath)
			}
			return current, module.Module.Mod.Path, true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", "", false, fmt.Errorf("read %s: %w", modPath, err)
		}
		if filepath.Dir(current) == current {
			return "", "", false, nil
		}
	}
}
