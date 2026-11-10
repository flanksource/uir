package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/query"
	"github.com/spf13/cobra"
)

type moduleEffectsOptions struct {
	Selector   string `flag:"selector" args:"true" help:"Symbol expression naming one function, method or package-level variable; also the first argument"`
	Symbol     string `flag:"symbol" help:"Canonical symbol id, as a graph node or candidate carries it"`
	RootKey    string `flag:"root" help:"Module path to query; defaults to the module of the current checkout"`
	Location   string `flag:"location" help:"Registered checkout path; defaults to the current checkout"`
	SnapshotID string `flag:"snapshot" help:"Explicit immutable snapshot UUID"`
	allRootsOption
}

// moduleEffectsLong documents what an effect is and how far the answer can be trusted.
const moduleEffectsLong = `Reports what a symbol does beyond computing from its inputs and the indexed source, through
everything it calls, references as a function value, or reads or writes as a package-level variable,
however deep: the clock, randomness, the environment, the process, files, the network, databases,
package-level state, goroutines and channels. A symbol is deterministic when none of them applies;
lock and map iteration effects are reported but do not break determinism.

Library functions are classified by a built-in catalog of the standard library and gorm. "unknown"
marks what the index cannot see, so determinism is never claimed for it: a call through an interface,
a function value or a type parameter, a library function the catalog does not know, an unresolved
call, and code indexed before effects were recorded (reindex it). Each effect names the shortest path
to the symbol that has it itself.`

type moduleEffectsResult struct {
	query.EffectsResult
}

func (result moduleEffectsResult) Pretty() api.Text {
	if len(result.Candidates) > 0 {
		text := clicky.Text(fmt.Sprintf("%d candidates: narrow the selector or pass --symbol", len(result.Candidates)), uir.StyleBold)
		for _, candidate := range result.Candidates {
			text = text.NewLine().Append(candidate.QueryName).Space().Append(candidate.ID, uir.StyleMuted)
		}
		return text
	}
	if result.Symbol == nil {
		return clicky.Text("no indexed snapshot to resolve in", uir.StyleMuted)
	}
	verdict, style := "deterministic", uir.StyleDeterministic
	if !result.Deterministic {
		verdict, style = "not deterministic", uir.StyleNondeterministic
	}
	text := clicky.Text(result.Symbol.QueryName, uir.StyleBold).Space().Append(verdict, style)
	for _, effect := range result.Effects {
		text = text.NewLine().Append("  "+string(effect.Effect), uir.StyleBold).Space().Append(effect.Evidence).Space().
			Append("via "+strings.Join(effect.Path, " → "), uir.StyleMuted)
	}
	return text
}

func registerModuleEffectsCommand(root *cobra.Command) {
	command := clicky.AddNamedCommandWithContext("effects", root, moduleEffectsOptions{}, func(ctx context.Context, options moduleEffectsOptions) (moduleEffectsResult, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return moduleEffectsResult{}, err
		}
		scope, err := moduleScope(ctx, database, checkoutDefaults{Root: options.RootKey, Location: options.Location, Snapshot: options.SnapshotID, All: options.All})
		if err != nil {
			return moduleEffectsResult{}, err
		}
		pipeline, err := query.NewPipeline(database)
		if err != nil {
			return moduleEffectsResult{}, err
		}
		result, err := pipeline.Effects(ctx, query.EffectsOptions{Selector: options.Selector, Symbol: options.Symbol, Scope: scope})
		if err != nil {
			return moduleEffectsResult{}, queryStatusError(err)
		}
		return moduleEffectsResult{EffectsResult: result}, nil
	})
	command.Short = "Report a symbol's external effects and whether it is deterministic"
	command.Long = moduleEffectsLong
	command.Example = "  uir effects example.org/service/orders.Submit\n  uir effects --symbol <id> --format json\n  uir query \"SELECT symbol.query_name FROM func:* WHERE symbol.deterministic = TRUE\""
	command.Args = cobra.MaximumNArgs(1)
}
