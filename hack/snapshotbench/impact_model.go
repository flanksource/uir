//go:build hack

package main

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/flanksource/uir/storage"
)

type impactOptions struct {
	Database string
	Output   string
	Label    string
	Samples  int
	Root     string
	From     string
	To       string
	Depth    int
	Limit    int
	Only     string
}

type impactRun struct {
	Version      int                    `json:"version"`
	StartedAt    time.Time              `json:"started_at"`
	Options      impactOptions          `json:"options"`
	CorpusSHA    string                 `json:"corpus_sha256"`
	ProducerSHA  string                 `json:"producer_sha256"`
	GoVersion    string                 `json:"go_version"`
	Platform     string                 `json:"platform"`
	GOMAXPROCS   int                    `json:"gomaxprocs"`
	From         storage.ModuleSnapshot `json:"from_snapshot"`
	To           storage.ModuleSnapshot `json:"to_snapshot"`
	Observations []impactObservation    `json:"observations"`
	Complete     bool                   `json:"complete"`
	Error        string                 `json:"error,omitempty"`
}

type impactObservation struct {
	Case            string                 `json:"case"`
	Mode            string                 `json:"mode"`
	Iteration       int                    `json:"iteration"`
	ElapsedNS       int64                  `json:"elapsed_ns"`
	AllocatedBytes  uint64                 `json:"allocated_bytes"`
	ResponseBytes   int                    `json:"response_bytes"`
	CurrentGraphSHA string                 `json:"current_graph_sha256,omitempty"`
	Nodes           int                    `json:"nodes,omitempty"`
	Edges           int                    `json:"edges,omitempty"`
	Unreadable      int                    `json:"unreadable_sources,omitempty"`
	Changed         bool                   `json:"changed,omitempty"`
	Pruned          *storage.PruneResult   `json:"pruned,omitempty"`
	StatsBefore     *storage.SnapshotStats `json:"stats_before,omitempty"`
	StatsAfter      *storage.SnapshotStats `json:"stats_after,omitempty"`
	StatePreserved  bool                   `json:"state_preserved,omitempty"`
}

type impactDistribution struct {
	Count  int     `json:"count"`
	Min    float64 `json:"min"`
	Median float64 `json:"median"`
	P95    float64 `json:"p95"`
	Max    float64 `json:"max"`
}

func impactSummary(values []float64) (impactDistribution, error) {
	if len(values) == 0 {
		return impactDistribution{}, fmt.Errorf("no samples")
	}
	ordered := slices.Clone(values)
	slices.Sort(ordered)
	middle := len(ordered) / 2
	median := ordered[middle]
	if len(ordered)%2 == 0 {
		median = (ordered[middle-1] + median) / 2
	}
	return impactDistribution{Count: len(ordered), Min: ordered[0], Median: median,
		P95: ordered[int(math.Ceil(0.95*float64(len(ordered))))-1], Max: ordered[len(ordered)-1]}, nil
}
