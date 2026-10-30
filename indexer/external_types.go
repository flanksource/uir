package indexer

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/flanksource/uir/internal/canonicalhash"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// ExternalIndexerVersion versions how Publish turns a publication into documents and hashes: it enters
// the configuration hash of every external snapshot and is the indexer version of every external
// document.
const ExternalIndexerVersion = "external-v8"

const (
	externalConfigurationVersion = "uir-external-configuration-v1"
	externalPackageInputVersion  = "uir-external-package-input-v1"
	externalExportShapeVersion   = "uir-export-external-v2"
)

// Publication is one root a non-Go producer publishes: what `uir import` reads and Publish writes. Its
// symbols are the identity of every symbol its documents name, declared or referenced, owners
// included: a document carries only canonical ids, and an id's module, package, kind, owner, and
// parameter types cannot be recovered from it. What a root declares, references, calls, and writes
// comes from its documents' content, exactly as for a Go module.
type Publication struct {
	Options PublishOptions `json:"options,omitzero"`
	// RootKey identifies the root, as a Go module path does; any non-empty string.
	RootKey string `json:"root_key"`
	// Name is the root's display name when the publication registers it.
	Name     string           `json:"name"`
	Location ExternalLocation `json:"location"`
	// Revision is the producer's revision of its sources, such as a commit; it may be empty. A
	// publication whose revision, documents, and sources equal the current head's is reported unchanged.
	Revision      string                     `json:"revision"`
	Kinds         []KindDeclaration          `json:"kinds"`
	Relationships []storage.RelationshipKind `json:"relationships,omitempty"`
	Symbols       []PublishedSymbol          `json:"symbols"`
	Documents     []PublishedDocument        `json:"documents"`
	Delta         *PublicationDelta          `json:"delta,omitempty"`
}

// PublicationDelta replaces complete packages and explicitly removes paths from
// Options.BaseSnapshotID. Omitted packages retain their sources and documents.
type PublicationDelta struct {
	DeletedPaths []string `json:"deleted_paths,omitempty"`
}

// ExternalLocation is where the producer reads the root from; URI is stored as the location's
// canonical path, with kind external.
type ExternalLocation struct {
	URI           string `json:"uri"`
	RepositoryURI string `json:"repository_uri,omitempty"`
}

// KindDeclaration is a custom symbol kind the publication uses, registered before its documents are
// validated; see storage.RegisterKind. Metrics names the non-negative integer metrics every declaration
// of the kind records in its document symbol's measures; a later declaration may add metrics but never
// remove or redescribe one.
type KindDeclaration struct {
	Schema   storage.JSON                  `json:"schema,omitempty"`
	Metrics  map[string]storage.MetricSpec `json:"metrics,omitempty"`
	Name     string                        `json:"name"`
	Category symbolhandle.Category         `json:"category"`
}

// PublishedSymbol is one symbol's identity, whose id is SymbolID(Identity), and its visibility,
// exported or internal.
type PublishedSymbol struct {
	Identity
	Visibility string `json:"visibility"`
}

// PublishedDocument is one source of the root: its root-relative slash path, the package it belongs to,
// its source text and that text's SHA-256, and the facts extracted from it in the typed document format
// (docs/symbol-index-storage.md). Documents of one package are replaced together, as a Go package's are.
type PublishedDocument struct {
	PathKey     string                  `json:"path_key"`
	PackagePath string                  `json:"package_path"`
	ContentHash string                  `json:"content_hash"`
	Source      string                  `json:"source"`
	Content     storage.DocumentContent `json:"content"`
	GitPath     string                  `json:"git_path,omitempty"`
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// validate checks what needs no database: required fields, kind names, and every document's path and
// hash.
func (publication Publication) validate() error {
	switch {
	case publication.RootKey == "":
		return errors.New("root_key is required")
	case strings.TrimSpace(publication.Name) == "":
		return errors.New("name is required")
	case publication.Location.URI == "":
		return errors.New("location.uri is required")
	case publication.Delta != nil && (!publication.Options.Historical || publication.Options.BaseSnapshotID == ""):
		return errors.New("delta publication requires a historical identity and options.base_snapshot_id")
	case len(publication.Documents) == 0 && publication.Delta == nil:
		return errors.New("documents must list at least one document")
	}
	kinds := make(map[string]bool, len(publication.Kinds))
	for _, kind := range publication.Kinds {
		if err := symbolhandle.ValidateCustomKind(kind.Name, kind.Category); err != nil {
			return fmt.Errorf("kind %q: %w", kind.Name, err)
		}
		if kinds[kind.Name] {
			return fmt.Errorf("kind %q is declared twice", kind.Name)
		}
		for name := range kind.Metrics {
			if err := storage.ValidateMetricName(name); err != nil {
				return fmt.Errorf("kind %q: %w", kind.Name, err)
			}
		}
		kinds[kind.Name] = true
	}
	paths := make(map[string]bool, len(publication.Documents))
	for _, document := range publication.Documents {
		if document.GitPath != "" && (publication.Location.RepositoryURI == "" || publication.Options.GitCommit == "") {
			return fmt.Errorf("document %q: git_path requires location.repository_uri and options.git_commit", document.PathKey)
		}
		if paths[document.PathKey] {
			return fmt.Errorf("document %q is listed twice", document.PathKey)
		}
		paths[document.PathKey] = true
		if err := document.validate(); err != nil {
			return fmt.Errorf("document %q: %w", document.PathKey, err)
		}
	}
	return nil
}

func (document PublishedDocument) validate() error {
	switch {
	case document.GitPath != "" && (!fs.ValidPath(document.GitPath) || document.GitPath == "." || strings.ContainsRune(document.GitPath, 0)):
		return errors.New("git_path must be a repository-relative slash-separated file path")
	case document.PathKey == "" || strings.Contains(document.PathKey, `\`) || path.IsAbs(document.PathKey) ||
		path.Clean(document.PathKey) != document.PathKey || document.PathKey == ".." || strings.HasPrefix(document.PathKey, "../"):
		return errors.New("path_key must be a relative slash-separated path inside the root")
	case document.PackagePath == "":
		return errors.New("package_path is required")
	case !sha256Hex.MatchString(document.ContentHash):
		return fmt.Errorf("content_hash %q is not a 64-character hex SHA-256", document.ContentHash)
	case hashBytes([]byte(document.Source)) != document.ContentHash:
		return fmt.Errorf("content_hash %s is not the SHA-256 of its source", document.ContentHash)
	}
	return nil
}

// externalInputHash digests (configuration_hash, package_path, sorted (path_key, content_hash, document
// content)): a package's documents are the producer's facts, so a changed document re-keys its package
// even when its source did not change.
func externalInputHash(configuration, packagePath string, files []discoveredFile, contents map[string]storage.JSON) string {
	sorted := append([]discoveredFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PathKey < sorted[j].PathKey })
	digest := canonicalhash.New(externalPackageInputVersion)
	digest.Text(configuration)
	digest.Text(packagePath)
	digest.Count(len(sorted))
	for _, file := range sorted {
		digest.Text(file.PathKey)
		digest.Text(file.ContentHash)
		digest.Text(file.GitPath)
		digest.Text(string(contents[file.PathKey]))
	}
	return digest.Sum()
}

// externalExportShape digests the sorted (id, public_hash) of a package's exported declarations.
func externalExportShape(documents []storage.DocumentContent) string {
	var declared []string
	for _, content := range documents {
		for _, symbol := range content.Symbols {
			if symbol.ID != nil && symbol.Visibility == "exported" {
				declared = append(declared, *symbol.ID+" "+symbol.PublicHash)
			}
		}
	}
	sort.Strings(declared)
	digest := canonicalhash.New(externalExportShapeVersion)
	digest.Count(len(declared))
	for _, entry := range declared {
		digest.Text(entry)
	}
	return digest.Sum()
}
