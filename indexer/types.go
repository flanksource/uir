// Package indexer incrementally projects Go syntax into immutable module snapshots.
package indexer

import (
	"errors"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

const ExtractorVersion = "go-ast-v3"

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
