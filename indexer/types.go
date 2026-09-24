// Package indexer incrementally projects type-checked Go facts into immutable module snapshots.
package indexer

import (
	"errors"
	"go/token"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"golang.org/x/tools/go/packages"
	"gorm.io/gorm"
)

// IndexerVersion versions the extractor and the document format it writes; it enters the configuration hash.
const IndexerVersion = "go-types-v1"

type Indexer struct {
	database     *gorm.DB
	loadPackages packageLoader
}

// packageLoader is the go/packages load a typed extraction runs; tests count calls through it.
type packageLoader func(config *packages.Config, patterns ...string) ([]*packages.Package, error)

func New(database *gorm.DB) (*Indexer, error) {
	if database == nil {
		return nil, errors.New("UIR index database is required")
	}
	return &Indexer{database: database, loadPackages: packages.Load}, nil
}

type fileIndex struct {
	PackagePath string
	PackageName string
	Nodes       []nodeSpec
	Calls       []callSpec
	fileSet     *token.FileSet
	content     []byte
}

type nodeSpec struct {
	declarationFacts
	Identifier     uir.Identifier
	ParentIdentity string
	ChildSlot      string
	Ordinal        int
	Payload        storage.JSON
	SemanticHash   string
	Field          *storage.Field
}

type callSpec struct {
	FromIdentity  string
	ToIdentifier  uir.Identifier
	LocalRoot     bool
	Resolvable    bool
	StatementPath string
	Span          storage.ByteSpan
	Text          string
}
