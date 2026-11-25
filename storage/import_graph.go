package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ImportGraphVersion versions the ImportGraph document; a reader refuses any other.
const ImportGraphVersion = 1

// ErrNoImportGraph marks a snapshot that recorded no import graph: it predates import graphs, or its
// producer reads no Go load.
var ErrNoImportGraph = errors.New("snapshot recorded no import graph")

// ImportGraph is the package import graph a typed Go load read: every package outside the standard
// library that a root's non-test package reaches, with the module it belongs to and the packages it
// imports. Modules and Packages are sorted and Imports are indexes into Packages, so equal graphs
// encode to equal bytes. A package with Own set is one of the root's own non-test packages; Dir is its
// directory relative to the root and File its representative non-test Go file (doc.go, else the first by name).
type ImportGraph struct {
	Version  int                  `json:"version"`
	Modules  []string             `json:"modules"`
	Packages []ImportGraphPackage `json:"packages"`
}

// ImportGraphPackage is one package of an ImportGraph.
type ImportGraphPackage struct {
	Path    string `json:"path"`
	Module  int    `json:"module"`
	Own     bool   `json:"own,omitempty"`
	Dir     string `json:"dir,omitempty"`
	File    string `json:"file,omitempty"`
	Imports []int  `json:"imports,omitempty"`
}

// ImportGraphBlob is one encoded import graph, shared by every snapshot that read it.
type ImportGraphBlob struct {
	GraphHash string `gorm:"column:graph_hash;primaryKey"`
	Graph     []byte `gorm:"column:graph"`
}

func (ImportGraphBlob) TableName() string { return "import_graph_blobs" }

// SnapshotImportGraph names the import graph of a snapshot.
type SnapshotImportGraph struct {
	SnapshotID uuid.UUID `gorm:"column:snapshot_id;primaryKey"`
	GraphHash  string    `gorm:"column:graph_hash"`
}

func (SnapshotImportGraph) TableName() string { return "snapshot_import_graphs" }

// Encode returns the graph's SHA-256 and its gzip-compressed JSON.
func (graph ImportGraph) Encode() (string, []byte, error) {
	if graph.Version != ImportGraphVersion {
		return "", nil, fmt.Errorf("import graph version %d, expected %d", graph.Version, ImportGraphVersion)
	}
	document, err := json.Marshal(graph)
	if err != nil {
		return "", nil, fmt.Errorf("encode import graph: %w", err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(document); err != nil {
		return "", nil, fmt.Errorf("compress import graph: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", nil, fmt.Errorf("compress import graph: %w", err)
	}
	digest := sha256.Sum256(document)
	return hex.EncodeToString(digest[:]), compressed.Bytes(), nil
}

// DecodeImportGraph is the inverse of ImportGraph.Encode.
func DecodeImportGraph(compressed []byte) (ImportGraph, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return ImportGraph{}, fmt.Errorf("decompress import graph: %w", err)
	}
	document, err := io.ReadAll(reader)
	if err != nil {
		return ImportGraph{}, fmt.Errorf("decompress import graph: %w", err)
	}
	var graph ImportGraph
	if err := json.Unmarshal(document, &graph); err != nil {
		return ImportGraph{}, fmt.Errorf("decode import graph: %w", err)
	}
	if graph.Version != ImportGraphVersion {
		return ImportGraph{}, fmt.Errorf("import graph version %d, expected %d", graph.Version, ImportGraphVersion)
	}
	return graph, nil
}

// SaveSnapshotImportGraph records graph as the snapshot's import graph, replacing a previous record.
func SaveSnapshotImportGraph(ctx context.Context, database *gorm.DB, snapshot uuid.UUID, graph ImportGraph) error {
	hash, encoded, err := graph.Encode()
	if err != nil {
		return fmt.Errorf("record the import graph of snapshot %s: %w", snapshot, err)
	}
	blob := ImportGraphBlob{GraphHash: hash, Graph: encoded}
	if err := database.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&blob).Error; err != nil {
		return fmt.Errorf("store the import graph of snapshot %s: %w", snapshot, err)
	}
	upsert := clause.OnConflict{Columns: []clause.Column{{Name: "snapshot_id"}}, DoUpdates: clause.AssignmentColumns([]string{"graph_hash"})}
	if err := database.WithContext(ctx).Clauses(upsert).Create(&SnapshotImportGraph{SnapshotID: snapshot, GraphHash: hash}).Error; err != nil {
		return fmt.Errorf("record the import graph of snapshot %s: %w", snapshot, err)
	}
	return nil
}

// LoadSnapshotImportGraph reads the snapshot's import graph, or ErrNoImportGraph.
func LoadSnapshotImportGraph(ctx context.Context, database *gorm.DB, snapshot uuid.UUID) (ImportGraph, error) {
	var blob ImportGraphBlob
	err := database.WithContext(ctx).Table("snapshot_import_graphs AS recorded").Select("blob.*").
		Joins("JOIN import_graph_blobs AS blob ON blob.graph_hash = recorded.graph_hash").
		Where("recorded.snapshot_id = ?", snapshot).Take(&blob).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ImportGraph{}, fmt.Errorf("snapshot %s: %w; reindex or republish its checkout with this version of uir", snapshot, ErrNoImportGraph)
	}
	if err != nil {
		return ImportGraph{}, fmt.Errorf("load the import graph of snapshot %s: %w", snapshot, err)
	}
	graph, err := DecodeImportGraph(blob.Graph)
	if err != nil {
		return ImportGraph{}, fmt.Errorf("import graph of snapshot %s: %w", snapshot, err)
	}
	return graph, nil
}
