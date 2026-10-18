// Package indexer incrementally projects type-checked Go facts into immutable module snapshots.
package indexer

import (
	"errors"

	"golang.org/x/tools/go/packages"
	"gorm.io/gorm"
)

// IndexerVersion versions the extractor and the document format it writes; it enters the configuration hash.
const IndexerVersion = "go-types-v5"

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
