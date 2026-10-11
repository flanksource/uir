package main

import (
	"time"

	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
)

type corpus struct {
	Name, Producer, Source, Tip, RootKey string
	Commits                              []string
	OriginalStatus                       string
	OriginalStatusAfter                  string
}

type runManifest struct {
	Version               int
	StartedAt             time.Time
	Directory, WorkingDir string
	ReusedGoRun           string `json:"ReusedGoRun,omitempty"`
	Runner, OIPA, Gavel   string
	Fixture               string
	Fingerprints          map[string]string
	GoVersion, Cache      string
	Transitions           int
	Corpora               []corpus
	Cases                 []measurement
}

type measurement struct {
	ID, Corpus, Producer, Path, Kind, Commit, RootKey string
	Checkout, Database, Directory, OIPA               string
	CheckoutMS                                        float64
}

type databaseState struct {
	Rows  map[string]int64 `json:"rows"`
	Heads map[string]head  `json:"heads"`
	Bytes int64            `json:"bytes"`
	WAL   int64            `json:"wal_bytes"`
}

type head struct {
	Snapshot string `json:"snapshot"`
	Version  int64  `json:"version"`
}

type observation struct {
	Case           measurement                  `json:"case"`
	OK             bool                         `json:"ok"`
	Error          string                       `json:"error,omitempty"`
	OpenMS         float64                      `json:"open_ms"`
	IndexMS        float64                      `json:"index_ms"`
	Before         databaseState                `json:"before"`
	After          databaseState                `json:"after"`
	Delta          map[string]int64             `json:"delta"`
	Modules        []indexer.ModuleResult       `json:"modules"`
	Snapshots      []storage.ModuleSnapshotView `json:"snapshots"`
	SourceVerified int                          `json:"source_verified"`
	CPU            string                       `json:"cpu_profile,omitempty"`
	Heap           string                       `json:"heap_profile,omitempty"`
	Trace          string                       `json:"trace"`
	Command        []string                     `json:"command"`
	Coverage       *producerCoverage            `json:"producer_coverage,omitempty"`
	Evidence       *fixtureEvidence             `json:"evidence,omitempty"`
	Phases         map[string]phaseTotal        `json:"phases,omitempty"`
}

type producerCoverage struct {
	Included []string `json:"included"`
	Omitted  []string `json:"omitted"`
}
