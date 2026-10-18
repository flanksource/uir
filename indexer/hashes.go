package indexer

import (
	"sort"
	"strconv"

	"github.com/flanksource/uir/internal/canonicalhash"
)

const (
	publicHashVersion        = "uir-public-hash-v1"
	privateHashVersion       = "uir-private-hash-v1"
	packageInputHashVersion  = "uir-package-input-v5"
	contextHashVersion       = "uir-context-hash-v1"
	contentSetHashVersion    = "uir-content-set-v1"
	configurationHashVersion = "uir-configuration-v1"

	exportShapeVersion           = "uir-export-shape-v4"
	standardExportShapeVersion   = "uir-export-std-v1"
	moduleExportShapeVersion     = "uir-export-module-v1"
	unprovenExportShapeVersion   = "uir-export-unproven-v1"
	unresolvedExportShapeVersion = "uir-export-unresolved-v1"

	siblingSourceVersion = "uir-sibling-source-v1"
)

// contentSetHash digests the module's (path_key, package_path, content_hash) files in path order and
// its dependency manifests, so a dependency bump changes the content set without any source delta.
func contentSetHash(files []discoveredFile, manifests []manifestFile) string {
	digest := canonicalhash.New(contentSetHashVersion)
	digest.Count(len(files))
	for _, file := range files {
		digest.Text(file.PathKey)
		digest.Text(file.PackagePath)
		digest.Text(file.ContentHash)
	}
	digest.Count(len(manifests))
	for _, manifest := range manifests {
		digest.Text(manifest.Key)
		digest.Text(manifest.ContentHash)
	}
	return digest.Sum()
}

// configurationHash digests (indexer_version, include_tests, GOOS, GOARCH, CGO_ENABLED, toolchain
// version, sorted build tags).
func configurationHash(includeTests bool, variant buildVariant) string {
	tags := append([]string(nil), variant.BuildTags...)
	sort.Strings(tags)
	digest := canonicalhash.New(configurationHashVersion)
	digest.Text(IndexerVersion)
	digest.Text(strconv.FormatBool(includeTests))
	digest.Text(variant.GOOS)
	digest.Text(variant.GOARCH)
	digest.Text(variant.CGOEnabled)
	digest.Text(variant.GoVersion)
	digest.Text(variant.GoFlags)
	digest.Count(len(tags))
	for _, tag := range tags {
		digest.Text(tag)
	}
	if variant.GoWorkOff {
		digest.Text("standalone-module")
	}
	return digest.Sum()
}

// importShape is one direct import of a package and the export shape it presents.
type importShape struct {
	Path            string
	ExportShapeHash string
}

// packageInputHash digests (configuration_hash, package_path, sorted (path_key, content_hash),
// sorted (import path, export_shape_hash)): everything extracting the package depends on.
func packageInputHash(configurationHash, packagePath string, files []discoveredFile, imports []importShape) string {
	sorted := append([]discoveredFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PathKey < sorted[j].PathKey })
	sortedImports := append([]importShape(nil), imports...)
	sort.Slice(sortedImports, func(i, j int) bool {
		left, right := sortedImports[i], sortedImports[j]
		return left.Path < right.Path || (left.Path == right.Path && left.ExportShapeHash < right.ExportShapeHash)
	})
	digest := canonicalhash.New(packageInputHashVersion)
	digest.Text(configurationHash)
	digest.Text(packagePath)
	digest.Count(len(sorted))
	for _, file := range sorted {
		digest.Text(file.PathKey)
		digest.Text(file.ContentHash)
	}
	digest.Count(len(sortedImports))
	for _, imported := range sortedImports {
		digest.Text(imported.Path)
		digest.Text(imported.ExportShapeHash)
	}
	return digest.Sum()
}

// contextHash digests (configuration_hash, sorted (package_path, input_hash)) of every package in a module.
func contextHash(configurationHash string, inputHashes map[string]string) string {
	packages := make([]string, 0, len(inputHashes))
	for packagePath := range inputHashes {
		packages = append(packages, packagePath)
	}
	sort.Strings(packages)
	digest := canonicalhash.New(contextHashVersion)
	digest.Text(configurationHash)
	digest.Count(len(packages))
	for _, packagePath := range packages {
		digest.Text(packagePath)
		digest.Text(inputHashes[packagePath])
	}
	return digest.Sum()
}
