//go:build hack

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/flanksource/uir/query"
	"gorm.io/gorm"
)

var impactSelectors = []string{
	"github.com/flanksource/captain/pkg/ai.NewAgent",
	"github.com/flanksource/captain/pkg/ai/agent.Runner.Run",
	"github.com/flanksource/captain/cmd/captain/internal/rootcmd.RegisterAIRuntimeCommands",
}

func impactGraphs(ctx context.Context, run *impactRun, database *gorm.DB) error {
	for _, selector := range impactSelectors {
		for _, mode := range []string{"cold-pipeline", "warm-pipeline"} {
			if err := impactGraphCohort(ctx, run, database, selector, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

func impactGraphCohort(ctx context.Context, run *impactRun, database *gorm.DB, selector, mode string) error {
	pipeline, err := query.NewPipeline(database)
	if err != nil {
		return err
	}
	options := query.GraphOptions{Selector: selector, Depth: run.Options.Depth, Limit: run.Options.Limit,
		Scope: query.ModuleScopeOptions{SnapshotID: run.To.ID.String()},
		Since: &query.GraphSince{Revision: run.Options.From, SnapshotID: run.From.ID}}
	var result query.GraphResult
	operation := func() ([]byte, error) {
		if mode == "cold-pipeline" {
			if pipeline, err = query.NewPipeline(database); err != nil {
				return nil, err
			}
		}
		if result, err = pipeline.Graph(ctx, options); err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
	if _, err := operation(); err != nil {
		return fmt.Errorf("warm up %s: %w", selector, err)
	}
	for iteration := range run.Options.Samples {
		observed, err := timeImpact(operation)
		if err != nil {
			return err
		}
		if result.Since == nil || len(result.Roots) != 1 || result.Since.FromSnapshotID != run.From.ID.String() || result.Since.ToSnapshotID != run.To.ID.String() {
			return fmt.Errorf("%s returned an incomplete change query", selector)
		}
		body, err := json.Marshal(result.Graph)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		observed.Case, observed.Mode, observed.Iteration = selector, mode, iteration
		observed.CurrentGraphSHA, observed.Nodes, observed.Edges = hex.EncodeToString(hash[:]), len(result.Nodes), len(result.Edges)
		observed.Unreadable, observed.Changed = len(result.Omitted.UnreadableSource), result.Since.Changed
		if err := recordImpact(run, observed); err != nil {
			return err
		}
	}
	return nil
}
