package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// allRootsOption is the flag of a command whose empty --root means every registered root: on the
// command line inside a registered checkout it scopes to that checkout unless --all is set.
type allRootsOption struct {
	All bool `flag:"all" help:"Every registered root instead of the current checkout"`
}

// checkoutDefaults is the --root and --location a command was given, and how an empty root defaults.
type checkoutDefaults struct {
	Root     string
	Location string
	// Snapshot is an explicit snapshot UUID, which names its own root and checkout.
	Snapshot string
	// All selects every registered root instead of the working directory's checkout.
	All bool
	// Required makes an empty root outside a registered checkout an error rather than every root.
	Required bool
}

// defaultCheckout fills an empty --root and --location from the registered checkout enclosing the
// working directory. Over HTTP the server's working directory is not the caller's, so it only
// validates. An explicit --location is canonicalized, since checkouts are matched by exact path.
func defaultCheckout(ctx context.Context, database *gorm.DB, defaults checkoutDefaults) (string, string, error) {
	if defaults.All && (defaults.Root != "" || defaults.Location != "" || defaults.Snapshot != "") {
		return "", "", errors.New("--all cannot be combined with --root, --location, or --snapshot")
	}
	if detachedRequest(ctx) && defaults.Required && defaults.Root == "" {
		return "", "", entity.NewStatusError(http.StatusBadRequest, "missing_root", "root is required")
	}
	if detachedRequest(ctx) || defaults.All || defaults.Snapshot != "" {
		return defaults.Root, defaults.Location, nil
	}
	location, err := canonicalCheckout(ctx, database, defaults.Location)
	if err != nil {
		return "", "", err
	}
	if location != "" && (defaults.Root != "" || !defaults.Required) {
		return defaults.Root, location, nil
	}
	directory, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("resolve the working directory for the default --root: %w", err)
	}
	checkout, found, err := indexer.LocateCheckout(ctx, database, directory)
	if err != nil {
		return "", "", err
	}
	switch {
	case !found && defaults.Root == "" && defaults.Required:
		return "", "", fmt.Errorf("--root is required: %s is not inside a registered checkout (run uir add . or pass --root)", directory)
	case !found || defaults.Root != "" && checkout.RootKey != defaults.Root:
		return defaults.Root, location, nil
	case location == "":
		return checkout.RootKey, checkout.Location, nil
	}
	return checkout.RootKey, location, nil
}

// moduleScope is the query scope of a command over every root: its --root, --location, and --snapshot
// after defaultCheckout.
func moduleScope(ctx context.Context, database *gorm.DB, defaults checkoutDefaults) (query.ModuleScopeOptions, error) {
	rootKey, location, err := defaultCheckout(ctx, database, defaults)
	return query.ModuleScopeOptions{RootKey: rootKey, Location: location, SnapshotID: defaults.Snapshot}, err
}

// canonicalCheckout is location as registered: unchanged when a checkout is registered at exactly
// that path (an external producer's URI is not a directory), else its absolute, symlink-free path.
func canonicalCheckout(ctx context.Context, database *gorm.DB, location string) (string, error) {
	if location == "" {
		return "", nil
	}
	var registered int64
	if err := database.WithContext(ctx).Model(&storage.ModuleLocation{}).Where("canonical_path = ?", location).Count(&registered).Error; err != nil {
		return "", fmt.Errorf("look up checkout %q: %w", location, err)
	}
	if registered > 0 {
		return location, nil
	}
	absolute, err := filepath.Abs(location)
	if err != nil {
		return "", fmt.Errorf("resolve --location %q: %w", location, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("canonicalize --location %q: %w", location, err)
	}
	return canonical, nil
}
