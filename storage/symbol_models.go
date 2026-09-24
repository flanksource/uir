package storage

import "github.com/google/uuid"

// Symbol is a global canonical symbol identity; typed extraction populates it.
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
}

func (Symbol) TableName() string { return "symbols" }

// Document holds the facts extracted from one file version under one package input hash.
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
}

func (Document) TableName() string { return "documents" }

// SymbolPosting is the inverted index from a symbol to the documents that define or mention it.
type SymbolPosting struct {
	DocumentID      uuid.UUID `gorm:"column:document_id;primaryKey"`
	RootID          uuid.UUID `gorm:"column:root_id"`
	SymbolID        string    `gorm:"column:symbol_id;primaryKey"`
	Role            string    `gorm:"column:role;primaryKey"`
	OccurrenceCount int       `gorm:"column:occurrence_count"`
}

func (SymbolPosting) TableName() string { return "symbol_postings" }

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
