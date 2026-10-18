package storage

import (
	"fmt"

	"github.com/google/uuid"
)

// Symbol is a global canonical symbol identity; typed extraction populates it. ID, the SHA-256 of the
// canonical key, is the identity everywhere outside this database. Handle is the database-local
// H64b surrogate (see storage/symbolhandle) that postings and deltas store instead. Kind names a
// builtin or registered kind in symbol_kinds.
type Symbol struct {
	ID              string  `gorm:"column:id;primaryKey"`
	IdentityVersion int     `gorm:"column:identity_version"`
	CanonicalKey    string  `gorm:"column:canonical_key"`
	ModuleKey       string  `gorm:"column:module_key"`
	PackagePath     string  `gorm:"column:package_path"`
	Kind            string  `gorm:"column:kind"`
	OwnerID         *string `gorm:"column:owner_id"`
	Name            string  `gorm:"column:name"`
	SearchName      string  `gorm:"column:search_name"`
	Visibility      string  `gorm:"column:visibility"`
	ParameterTypes  JSON    `gorm:"column:parameter_types"`
	Handle          int64   `gorm:"column:handle"`
}

func (Symbol) TableName() string { return "symbols" }

// Document holds the facts extracted from one file version. It is immutable and content-addressed:
// ContentDigest (DocumentDigest) keys it within its root and path, so snapshots whose package inputs
// differ but whose rendering of the file is identical share it, its ordinal, and its postings. InputHash
// is provenance, the package input hash it was first rendered under; snapshots bind documents through
// document deltas, never by input hash. ContentDigest is nil only on a pre-membership duplicate (see
// 18_document_membership.hcl). Ordinal is its dense, database-local number (from 1) that postings and
// document deltas reference.
type Document struct {
	ID               uuid.UUID `gorm:"column:id;primaryKey"`
	RootID           uuid.UUID `gorm:"column:root_id"`
	PathKey          string    `gorm:"column:path_key"`
	SourceRevisionID uuid.UUID `gorm:"column:source_revision_id"`
	PackagePath      string    `gorm:"column:package_path"`
	InputHash        string    `gorm:"column:input_hash"`
	IndexerVersion   string    `gorm:"column:indexer_version"`
	Coverage         Coverage  `gorm:"column:coverage"`
	SymbolCount      int       `gorm:"column:symbol_count"`
	OccurrenceCount  int       `gorm:"column:occurrence_count"`
	Content          JSON      `gorm:"column:content"`
	Ordinal          int64     `gorm:"column:ordinal"`
	ContentDigest    *string   `gorm:"column:content_digest"`
}

func (Document) TableName() string { return "documents" }

// DocumentDelta is one change of a snapshot's path → document membership against its base: a set
// binding the path to a document by ordinal, or a delete tombstone for a path only the base served.
type DocumentDelta struct {
	SnapshotOrdinal int64           `gorm:"column:snapshot_ordinal;primaryKey;autoIncrement:false"`
	RootID          uuid.UUID       `gorm:"column:root_id"`
	PathKey         string          `gorm:"column:path_key;primaryKey"`
	DocumentOrdinal *int64          `gorm:"column:document_ordinal"`
	Operation       SourceOperation `gorm:"column:operation"`
}

func (DocumentDelta) TableName() string { return "document_deltas" }

// DocumentMembership is the one row recording the document membership cutover; see
// 18_document_membership.hcl.
type DocumentMembership struct {
	ID              int32 `gorm:"column:id;primaryKey;autoIncrement:false"`
	DigestedThrough int64 `gorm:"column:digested_through"`
	MembersThrough  int64 `gorm:"column:members_through"`
	Complete        bool  `gorm:"column:complete"`
}

func (DocumentMembership) TableName() string { return "document_membership" }

// PostingRole is the stored code of a posting's role.
type PostingRole int32

const (
	RoleDefinition         PostingRole = 0
	RoleReference          PostingRole = 1
	RoleImplements         PostingRole = 2
	RoleEmbeds             PostingRole = 3
	RoleRelationshipSource PostingRole = 4
	RoleRelationshipTarget PostingRole = 5
)

var postingRoleNames = [...]string{"definition", "reference", "implements", "embeds", "relationship_source", "relationship_target"}

func ParsePostingRole(name string) (PostingRole, error) {
	for code, known := range postingRoleNames {
		if known == name {
			return PostingRole(code), nil
		}
	}
	return 0, fmt.Errorf("unknown posting role %q", name)
}

func (role PostingRole) String() string {
	if role >= 0 && int(role) < len(postingRoleNames) {
		return postingRoleNames[role]
	}
	return fmt.Sprintf("PostingRole(%d)", int32(role))
}

// SymbolPosting is the inverted index from a symbol handle to the documents that define, reference, or
// implement it; RootOrdinal is the document's root, so a lookup can stay inside one root.
type SymbolPosting struct {
	DocumentOrdinal int64       `gorm:"column:document_ordinal;primaryKey;autoIncrement:false"`
	RootOrdinal     int32       `gorm:"column:root_ordinal"`
	SymbolHandle    int64       `gorm:"column:symbol_handle;primaryKey;autoIncrement:false"`
	Role            PostingRole `gorm:"column:role;primaryKey;autoIncrement:false"`
	OccurrenceCount int         `gorm:"column:occurrence_count"`
}

func (SymbolPosting) TableName() string { return "symbol_postings" }

// SymbolModule numbers a module key for the handle's module field.
type SymbolModule struct {
	Number    int32  `gorm:"column:number;primaryKey;autoIncrement:false"`
	ModuleKey string `gorm:"column:module_key"`
}

func (SymbolModule) TableName() string { return "symbol_modules" }

// SymbolPackage numbers a package path within its module for the handle's package field.
type SymbolPackage struct {
	ModuleNumber int32  `gorm:"column:module_number;primaryKey;autoIncrement:false"`
	Number       int32  `gorm:"column:number;primaryKey;autoIncrement:false"`
	PackagePath  string `gorm:"column:package_path"`
}

func (SymbolPackage) TableName() string { return "symbol_packages" }

// SymbolDelta is one change of a snapshot's defined-symbol set against its base: a set with the
// symbol's public, private, and content fingerprints, or a delete tombstone without them. It shares the
// set/delete vocabulary of source deltas, and is keyed by the snapshot's and root's ordinals.
type SymbolDelta struct {
	SnapshotOrdinal int64           `gorm:"column:snapshot_ordinal;primaryKey;autoIncrement:false"`
	RootOrdinal     int32           `gorm:"column:root_ordinal"`
	SymbolHandle    int64           `gorm:"column:symbol_handle;primaryKey;autoIncrement:false"`
	Operation       SourceOperation `gorm:"column:operation"`
	PublicFP        *int64          `gorm:"column:public_fp"`
	PrivateFP       *int64          `gorm:"column:private_fp"`
	ContentFP       *int64          `gorm:"column:content_fp"`
}

func (SymbolDelta) TableName() string { return "symbol_deltas" }

// ImplementsDelta is one change of a snapshot's implements facts against its base: a set when a type
// the snapshot declares satisfies the interface and the base's facts lack the pair, or a delete
// tombstone for a pair only the base's facts hold.
type ImplementsDelta struct {
	SnapshotOrdinal int64           `gorm:"column:snapshot_ordinal;primaryKey;autoIncrement:false"`
	RootOrdinal     int32           `gorm:"column:root_ordinal"`
	TypeHandle      int64           `gorm:"column:type_handle;primaryKey;autoIncrement:false"`
	InterfaceHandle int64           `gorm:"column:interface_handle;primaryKey;autoIncrement:false"`
	Operation       SourceOperation `gorm:"column:operation"`
}

func (ImplementsDelta) TableName() string { return "implements_deltas" }

// ImplementsFacts marks a snapshot whose implements facts were computed, with the number of pairs its
// effective facts hold.
type ImplementsFacts struct {
	SnapshotOrdinal int64 `gorm:"column:snapshot_ordinal;primaryKey;autoIncrement:false"`
	RootOrdinal     int32 `gorm:"column:root_ordinal"`
	PairCount       int64 `gorm:"column:pair_count"`
}

func (ImplementsFacts) TableName() string { return "implements_facts" }

// PackageCoverage records, per snapshot, each package's analysis: the input hash its documents were
// rendered under, its coverage, file count, and diagnostics. Document membership is recorded separately,
// in document deltas.
type PackageCoverage struct {
	SnapshotID      uuid.UUID `gorm:"column:snapshot_id;primaryKey"`
	RootID          uuid.UUID `gorm:"column:root_id"`
	PackagePath     string    `gorm:"column:package_path;primaryKey"`
	InputHash       string    `gorm:"column:input_hash"`
	ExportShapeHash *string   `gorm:"column:export_shape_hash"`
	Coverage        Coverage  `gorm:"column:coverage"`
	FileCount       int       `gorm:"column:file_count"`
	Diagnostics     JSON      `gorm:"column:diagnostics"`
}

func (PackageCoverage) TableName() string { return "package_coverage" }

// SnapshotSiblingShape records a workspace sibling package a snapshot's typed load read: the export
// shape it presented when the snapshot imports it directly, and the digest of its sources for every
// package of the closure. Both are nil for a direct import written only in files no variant compiled.
type SnapshotSiblingShape struct {
	SnapshotID      uuid.UUID `gorm:"column:snapshot_id;primaryKey"`
	ImportPath      string    `gorm:"column:import_path;primaryKey"`
	ExportShapeHash *string   `gorm:"column:export_shape_hash"`
	SourceDigest    *string   `gorm:"column:source_digest"`
}

func (SnapshotSiblingShape) TableName() string { return "snapshot_sibling_shapes" }
