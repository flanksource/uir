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

// Document holds the facts extracted from one file version under one package input hash. Ordinal is
// its dense, database-local number (from 1) that postings reference.
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
}

func (Document) TableName() string { return "documents" }

// PostingRole is the stored code of a posting's role.
type PostingRole int32

const (
	RoleDefinition PostingRole = 0
	RoleReference  PostingRole = 1
	RoleImplements PostingRole = 2
	RoleEmbeds     PostingRole = 3
)

var postingRoleNames = [...]string{"definition", "reference", "implements", "embeds"}

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
// symbol's shape and body fingerprints, or a delete tombstone without them. It shares the set/delete
// vocabulary of source deltas, and is keyed by the snapshot's and root's ordinals.
type SymbolDelta struct {
	SnapshotOrdinal int64           `gorm:"column:snapshot_ordinal;primaryKey;autoIncrement:false"`
	RootOrdinal     int32           `gorm:"column:root_ordinal"`
	SymbolHandle    int64           `gorm:"column:symbol_handle;primaryKey;autoIncrement:false"`
	Operation       SourceOperation `gorm:"column:operation"`
	ShapeFP         *int64          `gorm:"column:shape_fp"`
	BodyFP          *int64          `gorm:"column:body_fp"`
}

func (SymbolDelta) TableName() string { return "symbol_deltas" }

// PackageCoverage records, per snapshot, the input hash that selects each package's documents.
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
