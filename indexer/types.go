// Package indexer incrementally projects Go syntax into immutable UIR snapshots.
package indexer

import (
	"errors"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

const ExtractorVersion = "go-ast-v3"

type Options struct {
	ProjectKey   string
	ProjectName  string
	Path         string
	RootKey      string
	IncludeTests bool
	Force        bool
}

type Result struct {
	ProjectKey    string  `json:"project_key"`
	SnapshotID    string  `json:"snapshot_id"`
	HeadVersion   int64   `json:"head_version"`
	Roots         int     `json:"roots"`
	Files         int     `json:"files"`
	ParsedFiles   int     `json:"parsed_files"`
	ReusedFiles   int     `json:"reused_files"`
	Nodes         int     `json:"nodes"`
	Relationships int     `json:"relationships"`
	Unchanged     bool    `json:"unchanged"`
	Timings       Timings `json:"timings"`
}

type Timings struct {
	DiscoveryMS   float64 `json:"discovery_ms"`
	LoadMS        float64 `json:"load_ms"`
	PreparationMS float64 `json:"preparation_ms"`
	PublicationMS float64 `json:"publication_ms"`
}

func (Result) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("project_key").Label("Project").Build(),
		api.Column("head_version").Label("Version").Type("int").Build(),
		api.Column("snapshot_id").Label("Snapshot").Build(),
		api.Column("roots").Label("Roots").Type("int").Build(),
		api.Column("files").Label("Files").Type("int").Build(),
		api.Column("parsed_files").Label("Parsed").Type("int").Build(),
		api.Column("reused_files").Label("Reused").Type("int").Build(),
		api.Column("nodes").Label("Nodes").Type("int").Build(),
		api.Column("relationships").Label("Relationships").Type("int").Build(),
		api.Column("unchanged").Label("Unchanged").Build(),
		api.Column("discovery_ms").Label("Discover ms").Build(),
		api.Column("load_ms").Label("Load ms").Build(),
		api.Column("preparation_ms").Label("Prepare ms").Build(),
		api.Column("publication_ms").Label("Publish ms").Build(),
	}
}

func (result Result) Row() map[string]any {
	return map[string]any{
		"project_key": result.ProjectKey, "head_version": result.HeadVersion, "snapshot_id": result.SnapshotID,
		"roots": result.Roots, "files": result.Files, "parsed_files": result.ParsedFiles,
		"reused_files": result.ReusedFiles, "nodes": result.Nodes,
		"relationships": result.Relationships, "unchanged": result.Unchanged,
		"discovery_ms": result.Timings.DiscoveryMS, "load_ms": result.Timings.LoadMS,
		"preparation_ms": result.Timings.PreparationMS, "publication_ms": result.Timings.PublicationMS,
	}
}

type Indexer struct {
	database *gorm.DB
}

func New(database *gorm.DB) (*Indexer, error) {
	if database == nil {
		return nil, errors.New("UIR index database is required")
	}
	return &Indexer{database: database}, nil
}

type fileIndex struct {
	PackagePath string
	PackageName string
	Nodes       []nodeSpec
	Calls       []callSpec
}

type nodeSpec struct {
	Identifier     uir.Identifier
	ParentIdentity string
	ChildSlot      string
	Ordinal        int
	Payload        storage.JSON
	SemanticHash   string
	StartLine      *int
	EndLine        *int
	Column         *int
	Field          *storage.Field
}

type callSpec struct {
	FromIdentity  string
	ToIdentifier  uir.Identifier
	ToRootKey     *string
	LocalRoot     bool
	Resolvable    bool
	StatementPath string
	StartLine     *int
	EndLine       *int
	Column        *int
	Text          string
}
