package indexer

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"sort"
	"strconv"
)

const (
	bodyHashVersion          = "uir-body-hash-v1"
	shapeHashVersion         = "uir-shape-hash-v1"
	packageInputHashVersion  = "uir-package-input-v3"
	contextHashVersion       = "uir-context-hash-v1"
	contentSetHashVersion    = "uir-content-set-v1"
	configurationHashVersion = "uir-configuration-v1"

	exportShapeVersion           = "uir-export-shape-v1"
	standardExportShapeVersion   = "uir-export-std-v1"
	moduleExportShapeVersion     = "uir-export-module-v1"
	unprovenExportShapeVersion   = "uir-export-unproven-v1"
	unresolvedExportShapeVersion = "uir-export-unresolved-v1"
)

// canonicalHash digests a versioned, length-delimited encoding, so no two field sequences share bytes.
type canonicalHash struct{ digest hash.Hash }

func newCanonicalHash(version string) canonicalHash {
	value := canonicalHash{digest: sha256.New()}
	value.text(version)
	return value
}

func (value canonicalHash) count(count int) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(count))
	_, _ = value.digest.Write(encoded[:])
}

func (value canonicalHash) text(text string) {
	value.count(len(text))
	_, _ = value.digest.Write([]byte(text))
}

func (value canonicalHash) sum() string { return hex.EncodeToString(value.digest.Sum(nil)) }

// contentSetHash digests the module's (path_key, package_path, content_hash) files in path order and
// its dependency manifests, so a dependency bump changes the content set without any source delta.
func contentSetHash(files []discoveredFile, manifests []manifestFile) string {
	digest := newCanonicalHash(contentSetHashVersion)
	digest.count(len(files))
	for _, file := range files {
		digest.text(file.PathKey)
		digest.text(file.PackagePath)
		digest.text(file.ContentHash)
	}
	digest.count(len(manifests))
	for _, manifest := range manifests {
		digest.text(manifest.Key)
		digest.text(manifest.ContentHash)
	}
	return digest.sum()
}

// configurationHash digests (indexer_version, include_tests, GOOS, GOARCH, CGO_ENABLED, toolchain
// version, sorted build tags).
func configurationHash(includeTests bool, variant buildVariant) string {
	tags := append([]string(nil), variant.BuildTags...)
	sort.Strings(tags)
	digest := newCanonicalHash(configurationHashVersion)
	digest.text(IndexerVersion)
	digest.text(strconv.FormatBool(includeTests))
	digest.text(variant.GOOS)
	digest.text(variant.GOARCH)
	digest.text(variant.CGOEnabled)
	digest.text(variant.GoVersion)
	digest.count(len(tags))
	for _, tag := range tags {
		digest.text(tag)
	}
	return digest.sum()
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
	digest := newCanonicalHash(packageInputHashVersion)
	digest.text(configurationHash)
	digest.text(packagePath)
	digest.count(len(sorted))
	for _, file := range sorted {
		digest.text(file.PathKey)
		digest.text(file.ContentHash)
	}
	digest.count(len(sortedImports))
	for _, imported := range sortedImports {
		digest.text(imported.Path)
		digest.text(imported.ExportShapeHash)
	}
	return digest.sum()
}

// contextHash digests (configuration_hash, sorted (package_path, input_hash)) of every package in a module.
func contextHash(configurationHash string, inputHashes map[string]string) string {
	packages := make([]string, 0, len(inputHashes))
	for packagePath := range inputHashes {
		packages = append(packages, packagePath)
	}
	sort.Strings(packages)
	digest := newCanonicalHash(contextHashVersion)
	digest.text(configurationHash)
	digest.count(len(packages))
	for _, packagePath := range packages {
		digest.text(packagePath)
		digest.text(inputHashes[packagePath])
	}
	return digest.sum()
}
