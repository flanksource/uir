package indexer

import (
	"context"
	"errors"
	"runtime/pprof"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Phase", func() {
	const outer, inner = "outer-phase", "inner-phase"

	phaseLabel := func(ctx context.Context) string {
		label, ok := pprof.Label(ctx, "phase")
		Expect(ok).To(BeTrue(), "the phase context carries no phase label")
		return label
	}

	It("runs fn under a context labelled with the phase, the innermost phase winning", func(ctx SpecContext) {
		var seen []string
		Expect(Phase(ctx, outer, func(ctx context.Context) error {
			seen = append(seen, phaseLabel(ctx))
			return Phase(ctx, inner, func(ctx context.Context) error {
				seen = append(seen, phaseLabel(ctx))
				return nil
			})
		})).To(Succeed())
		Expect(seen).To(Equal([]string{outer, inner}))
		_, labelled := pprof.Label(ctx, "phase")
		Expect(labelled).To(BeFalse(), "the caller's context must not gain the phase label")
	})

	It("returns the error fn returns", func(ctx SpecContext) {
		failure := errors.New("phase failed")
		Expect(Phase(ctx, outer, func(context.Context) error { return failure })).To(MatchError(failure))
	})
})
