package indexer

import (
	"fmt"
	"strings"

	"github.com/flanksource/uir/storage"
	"golang.org/x/tools/go/packages"
)

// absentPackageFailures are the go command's reports, as list errors, of an import that the commit's
// own build list decides cannot be satisfied: no selected module version, already fetched, contains the
// package, or the toolchain's standard library does not. Under -mod=readonly the command never looks a
// missing import up, and a module it could not fetch fails with the fetch's error instead.
var absentPackageFailures = []string{
	"import lookup disabled by -mod=", "no required module provides package", "missing go.sum entry for module providing package",
	"is not in std", "build constraints exclude all Go files in", "no Go files in", "import cycle not allowed",
	"use of internal package", "found packages ",
}

// infrastructureFailures are the texts of a failure of the machine rather than of the sources: the
// network, a proxy or the checksum database, the module cache, or the file system. A load error that
// carries one is never deterministic, whatever its kind.
var infrastructureFailures = []string{
	"dial tcp", "no such host", "i/o timeout", "connection refused", "connection reset", "TLS handshake", "proxyconnect",
	"Bad Gateway", "Service Unavailable", "Gateway Timeout", "Internal Server Error", "Too Many Requests", "unexpected EOF",
	"verifying ", "checksum mismatch", "SECURITY ERROR", "permission denied", "input/output error", "no space left on device",
	"too many open files", "read-only file system", "context deadline exceeded", "context canceled", "signal: killed",
	"does not contain modules listed in go.work", "outside modules listed in go.work",
}

// deterministicLoadError reports whether loading the same commit, configuration and dependency set
// again must report loadError again: a parse or type error, or a list error proving an import absent
// from the pinned modules. An unknown error, an unrecognised list error, or any error that names an
// infrastructure failure is not.
func deterministicLoadError(loadError packages.Error) bool {
	if containsAny(loadError.Msg, infrastructureFailures) {
		return false
	}
	switch loadError.Kind {
	case packages.ParseError, packages.TypeError:
		return true
	case packages.ListError:
		return containsAny(loadError.Msg, absentPackageFailures)
	default:
		return false
	}
}

// infrastructureListError is the failure of the first list error of pkg that names an infrastructure
// failure, or nil. Such an error is a failure of the machine for that package, so the load fails rather
// than publishing degraded coverage that is never a fact of the commit; an unrecognised list error
// still only makes the coverage not deterministic.
func infrastructureListError(pkg *packages.Package) error {
	for _, loadError := range pkg.Errors {
		if loadError.Kind == packages.ListError && containsAny(loadError.Msg, infrastructureFailures) {
			return fmt.Errorf("load package %q: infrastructure failure: %s", pkg.PkgPath, loadError.Msg)
		}
	}
	return nil
}

// deterministicDependency reports whether a dependency edge's unresolved reason, which the dependency
// set hash covers, is a fact of module selection or of which versions are registered, rather than a
// failed selection or an unreadable local dependency.
func deterministicDependency(edge storage.SnapshotDependency) bool {
	switch edge.UnresolvedReason {
	case "", reasonSelectedNotIndexed, reasonAbsentFromBuildList:
		return true
	default:
		return strings.HasPrefix(edge.UnresolvedReason, errVersionUnavailable.Error()+": ")
	}
}

// deterministicCoverage reports whether extracting the load's root again under the same commit,
// configuration and dependency set must reach the same coverage: the whole load ran, every error of
// every package it loaded is deterministic, and so is every dependency edge.
func (load *typedLoad) deterministicCoverage() bool {
	if load.loadFailure != "" {
		return false
	}
	for _, loadError := range load.errors {
		if !deterministicLoadError(loadError) {
			return false
		}
	}
	for _, dependency := range load.root.Dependencies {
		if !deterministicDependency(dependency.Edge) {
			return false
		}
	}
	return true
}

func containsAny(message string, fragments []string) bool {
	for _, fragment := range fragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
