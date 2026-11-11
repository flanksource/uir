package query

import (
	"context"

	"github.com/flanksource/uir/internal/progress"
)

type ruleQueryKey struct{}

func documentProgress(ctx context.Context, total int) (context.Context, func()) {
	if ctx.Value(ruleQueryKey{}) != nil {
		return ctx, func() {}
	}
	return progress.Begin(ctx, "Querying documents", total)
}
