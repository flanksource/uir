# Gortex feature analysis for UIR and arch-unit

_Source review: 2026-09-24. Gortex [`33107203de49`](https://github.com/zzet/gortex/tree/33107203de4900f1229a3eeff631ff9802c6725a); UIR `9e9e58e47ad5`; arch-unit `cdcc186ccbe0` plus its uncommitted UIR alias migration._

## Executive assessment

Gortex is a live, queryable code-intelligence graph optimized for repository navigation and agent workflows. UIR is a structured intermediate representation with a Go-specific, type-checked, snapshot-aware symbol index. arch-unit produces UIR across several languages and domains. In the inspected code, its output is not wired into UIR's Go module index; that is an inference from the separate extractor and indexing paths. These are complementary layers: Gortex offers useful query and evidence-handling patterns, while UIR's canonical identities and immutable snapshots solve different persistence and comparison problems. [Gortex architecture][g-architecture] · [UIR symbol model][u-symbols] · [arch-unit extractor interface][a-interface] · [UIR indexing][u-indexing]

The highest-value borrow is **honest uncertainty**: return proven callers separately from unresolved name-only candidates, preserve the origin of each fact, and mark impact or context results as incomplete when resolution or response limits prevent a complete answer. UIR already records unresolved occurrences and package coverage, so this can begin in its existing query pipeline. [Gortex candidate handling][g-name-only] · [Gortex impact result][g-impact] · [UIR occurrence][u-occurrence] · [UIR coverage][u-coverage]

The next useful product capability is a bounded, snapshot-scoped **symbol context response** for agents and the browser: declaration and source, callers, callees, related types, selected tests, and an explicit manifest of omitted or unavailable material. An editor overlay is valuable after that, provided it reindexes the affected Go package and checks the selected snapshot or file content before applying a preview. [Gortex context closure][g-closure] · [Gortex overlay][g-overlay] · [UIR package inputs][u-indexing]

## Scope and confidence

This is a source and documentation review of the cloned Gortex backend, the current UIR checkout, and the current arch-unit checkout. It is not a measured comparison of indexing speed, retrieval quality, memory use, or agent outcomes. Gortex's published timing and token-saving figures are upstream claims; its separate web UI repository was not reviewed. The arch-unit checkout has extensive uncommitted changes, including generated aliases to canonical UIR, so its observations describe the inspected worktree rather than a released version. Source links for its alias and current plugin protocol are local-checkout links; the other arch-unit links are pinned to its inspected commit. UIR's query frontend also has unrelated edits in progress; this report does not assess those edits. [Gortex performance table][g-performance] · [Gortex web UI boundary][g-server] · [arch-unit alias][a-alias]

“Present” below means an identifiable source or documented behavior, not parity in quality. “Opportunity” is a proposed addition and is not a claim that the current product already behaves that way.

## Feature comparison

### Extraction, identity, and resolution

| Capability | Gortex | UIR / arch-unit today | Assessment |
| --- | --- | --- | --- |
| Language breadth | Advertises 257 parsers in deep tree-sitter, regex, and signature-only tiers. Its committed resolution-parity fence covers 17 languages. [Catalog][g-features-graph] · [Fence][g-parity] | arch-unit registers Go, Java, JavaScript, Markdown, OpenAPI, Python, SQL, and XML extractors plus external plugins; UIR's persisted typed symbol index currently targets Go modules. [Registry][a-extractors] · [Indexing][u-indexing] | Declare extractor capabilities and achieved per-file coverage. A language count alone does not describe symbol or cross-file resolution quality. |
| Semantic model | File-anchored graph nodes and typed edges cover calls, imports, dataflow, tests, infrastructure, and contracts. [Schema][g-architecture] | UIR has structured node and statement hierarchies, identifiers, relationships, endpoint and record nodes. Its Go index stores declarations and occurrences per document. [Node][u-node] · [Statement][u-statement] · [Document][u-occurrence] | Keep UIR as the canonical semantic model; derive query edges and search projections from it when useful. |
| Durable symbol identity | Graph IDs include repository prefix and source file path. [Architecture][g-architecture] | Canonical Go symbol IDs encode module, package, kind, owner, name, and parameter types; declaration postings attach positions to snapshots. [Symbols][u-symbols] | Preserve UIR's path-independent identity across moves and historical snapshots. |
| Resolution evidence | Edges carry origin, confidence label, tier, and optional usage context. Query tools can filter by minimum tier. [Edge][g-edge] | Typed Go occurrences carry canonical IDs when proved; syntax documents have no canonical postings; package coverage reports partial, syntax-only, or excluded indexing. [Symbols][u-symbols] · [Coverage][u-coverage] | Add explicit provenance and resolution-outcome fields only where a consumer needs them; retain the proven/unproven boundary. |
| Unresolved uses | Unbound calls remain on unresolved placeholders. Queries count name-only candidates separately and copy them into weak result rows only when requested. [Implementation][g-name-only] | `DocumentOccurrence` already has nullable symbol, target, note, role, and enclosing location, but `ModuleQueryResult` exposes only aggregate package coverage. [Occurrence][u-occurrence] · [Result][u-query-result] | **High priority:** expose possible caller counts and optional weak witnesses without retargeting stored facts. |
| External and dynamic calls | External package nodes and provenance-tagged framework synthesizers preserve hops not resolved by ordinary AST matching. [Catalog][g-features-graph] | UIR stores typed Go calls and known interface dispatch; arch-unit can supply domain-specific relationships through extractors. [Query behavior][u-symbols] · [Extractor interface][a-interface] | Add domain-specific derived facts incrementally, with a producer and evidence tier. Avoid broad name-only synthetic edges. |
| Dataflow and concurrency | Graph edges include value flow, argument/return flow, spawns, sends, receives, reads, and writes; analysis tools include taint and concurrency heuristics. [Schema][g-architecture] · [Catalog][g-features-analysis] | UIR models statements and read/write/call relationships, but the Go symbol query is centered on proven declarations, occurrences, and calls. [Statement][u-statement] · [Query guide][u-query] | Candidate for a separate analysis projection. Establish source/sink semantics and false-positive tests before exposing safety verdicts. |
| Contracts and infrastructure | Normalizes HTTP, gRPC, topics, env vars, OpenAPI, and Temporal contracts into provider/consumer links; separately models infrastructure resources. [Contracts][g-contracts] · [Bridge][g-contract-bridge] · [Graph schema][g-architecture] | UIR has endpoint and record types; arch-unit has OpenAPI, SQL, XML, and a Kubernetes API-schema plugin. Those facts are not yet available to UIR's Go module query. [Endpoint][u-endpoint] · [arch-unit registry][a-extractors] · [Kubernetes plugin][a-k8s] · [UIR indexing][u-indexing] | Define an extraction-to-index bridge before cross-domain matching; keep derived contracts separate from core AST identity. |

### Index lifecycle and scope

| Capability | Gortex | UIR / arch-unit today | Assessment |
| --- | --- | --- | --- |
| Persistence | A daemon writes its current graph into SQLite and reconciles changed files on restart. [Architecture][g-architecture] | UIR publishes immutable snapshots with source deltas, reusable revisions/documents, per-location heads, and SQLite or PostgreSQL storage. [Module indexing][u-indexing] | Keep UIR snapshots. Borrow incremental invalidation and operational diagnostics where they fit its publication transaction. |
| Worktrees and multi-repo scope | Tracks checkout families and uses session workspace boundaries; responses disclose `scope_applied` and how to widen. [Checkouts][g-checkouts] · [Scope][g-multi-repo] | UIR registers module roots and checkout locations, selects primary heads by default, and supports explicit location or historical snapshot queries. [Module indexing][u-indexing] · [Query guide][u-query] | Return effective root/location/snapshot scope in every query response, including inferred defaults and incomplete roots. |
| Live unsaved changes | Session-bound overlay composes replacement or deleted files over a base graph; optional `base_sha` catches drift. [Layer][g-overlay] · [View][g-overlay-view] · [Drift guard][g-overlay-drift] | UIR reindexes and publishes a snapshot; its document input hash covers a whole Go package and imported export shapes. [Indexing][u-indexing] | Preview with a transient package-scoped overlay over a selected snapshot. Recompute affected package facts and never publish a preview as a normal snapshot. |
| Watch and notifications | Watcher updates the current graph; daemon emits diagnostics, readiness, stale-reference, and graph-invalidation events. [Catalog][g-features-runtime] | UIR has explicit `add`/`reindex`, a module browser, and persisted location heads. [Serve][u-serve] · [Indexing][u-indexing] | First add snapshot publication/status events if a live consumer needs them; watcher-driven publication requires a policy for dirty files and failed type checks. |
| Failure and coverage reporting | Gortex has extraction skips, provider and resolution outcomes, and per-language parity fixtures. [Catalog][g-features-runtime] · [Parity fence][g-parity] | UIR records `indexed`, `partial`, `syntax`, and `excluded` packages with diagnostics; arch-unit plugin metadata declares languages/input mode but no fidelity contract. [Coverage][u-coverage] · [Plugin info][a-plugin] | Define a small capability/coverage vocabulary shared by extractor, index, query, and UI. |

### Retrieval, analysis, and delivery

| Capability | Gortex | UIR / arch-unit today | Assessment |
| --- | --- | --- | --- |
| Search | Hybrid FTS5/BM25 and vectors, syntax-aware chunks, documentation corpus, and reranking. [Search design][g-search] | UIR has exact/prefix name lookup, typed selectors with globs, and snapshot-scoped semantic traversals. [Symbol model][u-symbols] · [Query guide][u-query] | Consider lexical source and documentation search first; benchmark identifier and natural-language tasks before vector indexing. |
| Query language | Symbol navigation, bounded graph traversal, and a small staged `graph_query` DSL. [Catalog][g-features-search] · [DSL][g-graph-query] | UIR already has a PEG grammar with `pkg:`, `mod:`, `func:`, `field:`, and `struct:` selectors; `pkg:` accepts `module-glob:relative-package-glob`; unary/binary relations, set operators, and bounded paths are documented. [Query guide][u-query] | Extend the existing PEG/query pipeline for proven facts. A second graph DSL would duplicate scope and completeness rules. |
| Impact and test targeting | Depth-bounded reverse reach, affected tests, `lower_bound`, `truncated`, and unresolved boundaries; optional precomputed reach accelerates hot paths. [Impact][g-impact] | UIR has snapshot symbol diffs and call-path queries; it does not expose a dedicated impact result with completeness fields. [Diff][u-diff] · [Query guide][u-query] | Traverse postings from a selected symbol for single-snapshot impact; use the diff to seed change-impact between commits. Profile before adding a precomputed reach index. |
| Agent context | `context_closure` and `smart_context` select focused source and related stubs under budgets; list responses can mark truncation. [Closure][g-closure] · [Budget][g-budget] | UIR returns structured query matches, declarations, symbols, coverage, and paths through CLI/HTTP and browser. [Result][u-query-result] · [Serve][u-serve] | Add one snapshot-aware context assembler and reuse it across API/CLI and future MCP exposure. |
| Interfaces | Shared MCP and versioned HTTP tool surfaces, daemon session isolation, agent hooks, and a separate web UI. [Server][g-server] · [Agent catalog][g-features-runtime] | UIR serves module and query operations through Clicky HTTP and has its own browser. [Serve][u-serve] | Expose narrow UIR operations through existing Clicky plumbing. Treat agent hooks and PR automation as separate products. |
| Refactoring and preview | Graph-aware edits, rename/move/inline, speculative `WorkspaceEdit`, and branchable overlays. [Catalog][g-features-workflow] | UIR has source locations and diffs; arch-unit has transformation/generation facilities, but UIR's symbol query is read-only. [Diff][u-diff] · [arch-unit interface][a-interface] | Begin with read-only preview and impact. Editing needs language-specific rewrite proof, drift guards, and explicit mutation boundaries. |
| Response economy | Token budgets, graded source fidelity, conditional fetch, result recutting, and GCX1 compact wire output. [Catalog][g-features-runtime] | UIR query limits are 1–1000, with `total`, `coverage`, and structured rows. [Query guide][u-query] | Add explicit pagination/truncation and source fidelity to context responses; measure wire costs before adding another serialization format. |
| Quality measurement | Per-language parity baseline/goldens plus a task evaluation design spanning explanations, refactors, localization, impact, and contracts. [Fence][g-parity] · [Methodology][g-evaluation] | UIR has focused Go query/index tests and model/schema checks; arch-unit has plugin/round-trip tests. [UIR verification][u-agents] · [Plugin protocol][a-plugin] | Borrow the evaluation *shape*: frozen cross-file-resolution fixtures and task-level answer checks on representative repos. Do not import headline scores as evidence. |

### Developer workflow and knowledge

| Capability | Gortex | UIR / arch-unit today | Assessment |
| --- | --- | --- | --- |
| Test and change intelligence | Test roles/runners, test-target queries, blame/churn, and co-change relations feed analysis. [Catalog][g-features-quality] | UIR can include Go test files when requested and compare symbol changes across snapshots. [Indexing][u-indexing] · [Diff][u-diff] | Test classification is a useful derived projection; coverage and co-change require separate evidence sources and freshness indicators. |
| Architecture guards | Configured dependency/layer rules and proposed-change checks operate on Gortex's graph. [Catalog][g-features-workflow] | arch-unit has a rule-violation analyzer seam, but its current UIR implementation returns no violations; UIR provides facts and snapshot comparisons. [Rule analyzer][a-rules] · [Diff][u-diff] | Implement policy evaluation in arch-unit after its UIR migration, using resolved facts and completeness metadata. |
| PR review and edits | Gortex exposes graph-grounded PR review, edit/refactor operations, and speculative previews. [Catalog][g-features-workflow] | arch-unit has generation and transformation seams; UIR's current indexed query surface is read-only. [Extractor interface][a-interface] · [Query guide][u-query] | A context/impact API can serve review tools without moving PR posting or code mutation into the UIR model. |
| Agent integration and memory | MCP tools, hooks, repository skills, notes, memories, and context export form an agent-oriented product surface. [Catalog][g-features-runtime] | UIR exposes CLI/HTTP queries and a browser. [Serve][u-serve] | Borrow compact, source-linked context outputs where agents need them; durable agent memory and hooks belong in an integration layer. |
| Knowledge artifacts | Documentation sections, specs, and configured non-code artifacts enter Gortex search and graph references. [Search docs][g-search-docs] · [Catalog][g-features-contract] | arch-unit extracts Markdown, OpenAPI, SQL, and XML into UIR structures; the Go symbol index does not search those corpora. [Registry][a-extractors] · [Query guide][u-query] | Add cross-domain retrieval through a separate searchable projection with explicit artifact provenance. |

## Recommended changes, in dependency order

### 1. Make query completeness explicit

Extend `ModuleQueryResult` with a small completeness object: effective scope, coverage summary, whether result rows hit the limit, and whether a traversal crossed unresolved calls. For incoming callers, derive a deduplicated `name_only_candidates` count from active call occurrences with no canonical target, restricted to the same selected roots/snapshot and compatible target name. Keep possible witnesses behind an explicit query option and label them as name matches. A caller count must not imply safety when the same scope contains unresolved sites or syntax-only packages. [UIR result][u-query-result] · [UIR occurrence][u-occurrence] · [Gortex candidate handling][g-name-only] · [Gortex impact result][g-impact]

Verification should include same-name symbols in separate packages, duplicate occurrences, partial and syntax-only packages, cross-root scope, and a result truncated at the query limit. The count is evidence about **possible** callers, never a lower bound on proven callers.

### 2. Record provenance at the producer boundary

Add a typed provenance vocabulary for facts that can have different proofs: Go type checker, unambiguous AST, extractor inference, text candidate, or synthetic framework/contract edge. arch-unit plugins should declare supported capabilities separately from per-file achieved coverage; an extractor's advertised language or input mode is insufficient to promise caller resolution. Query filters can then request a minimum proof tier without changing canonical symbol identity. [Gortex edge][g-edge] · [arch-unit plugin info][a-plugin] · [UIR coverage][u-coverage]

Scope this first to the fact types that need it: call/reference witnesses and derived contract links. Avoid stamping synthetic confidence percentages when the evidence is categorical.

### 3. Add a bounded symbol context and impact API

Given a canonical symbol and selected snapshot, return its declaration/source, direct proven callers and callees, selected related types, likely test files, coverage, and a manifest of omitted rows. Reuse the same scope and identity rules as `uir query`. Historical source must be read from hash-verified bytes for that snapshot; when those bytes are unavailable, return an explicit source-unavailable entry rather than current checkout text. Seed single-snapshot impact from the selected canonical symbol and traverse incoming facts to a configured depth. For change impact between two named commits in one root, use `symboldiff` to identify changed symbols before traversal. Return `lower_bound`, `truncated`, and unresolved boundaries; a partial answer must identify why it is partial. [UIR query][u-query] · [UIR browser source guard][u-serve] · [UIR diff][u-diff] · [Gortex closure][g-closure] · [Gortex impact][g-impact]

Start with on-demand traversal and a small deterministic budget. Measure query latency and corpus size before maintaining a reach cache or adding search ranking.

### 4. Add preview overlays after the read path is stable

Compose transient documents over a selected snapshot, with explicit replaced/deleted paths and a base-content hash guard. Reindex the affected Go package because UIR's typed document inputs are package-wide; include import export-shape invalidation when it changes. Return the same query/context shape with `view=overlay` and the base snapshot ID. [Gortex overlay][g-overlay] · [Gortex drift guard][g-overlay-drift] · [UIR input hashes][u-indexing]

### 5. Add cross-domain links and search selectively

First define how arch-unit's extracted UIR enters a snapshot-scoped derived index: source identity, extractor version, coverage, update transaction, and cross-root scope. Then normalize HTTP/OpenAPI, SQL, and other supported contracts into provider/consumer identities, recording source location and proof for each match. The Kubernetes plugin currently reads API schemas from a cluster; it is not a manifest graph extractor. For search, start with a lexical index over active declarations and documentation sections, then compare real tasks before adding embeddings. [Gortex contracts][g-contracts] · [UIR endpoint][u-endpoint] · [arch-unit extractors][a-extractors] · [Kubernetes plugin][a-k8s] · [Gortex search][g-search]

## Design boundaries and cautions

- **Identity and history:** retain UIR's path-independent canonical symbol IDs, immutable snapshots, and checkout heads. Gortex's path-anchored IDs and mutable current graph would change the meaning of UIR diffs and historical queries. [Gortex identity][g-architecture] · [UIR identity][u-symbols] · [UIR snapshots][u-indexing]
- **Language coverage:** distinguish “file parsed,” “declarations extracted,” “cross-file references resolved,” and “type-checked.” Gortex's advertised parser count and its 17-language parity fence measure different things. [Catalog][g-features-graph] · [Fence][g-parity]
- **Heuristic analysis:** health grades, centrality, dead-code warnings, taint paths, and automated edit/review verdicts need independent precision checks before UIR presents them as facts. Gortex exposes many such analyses, but this review did not measure their false-positive rate. [Analysis catalog][g-features-analysis]
- **Transport and agent workflow:** Clicky already provides CLI/API plumbing for UIR. MCP, hooks, PR review, persistent agent notes, and a compact wire codec are separate delivery and workflow decisions; they do not require changes to the UIR node model. [Gortex server][g-server] · [UIR serve][u-serve]
- **Performance:** the upstream Linux/VS Code timings and GCX1 token savings are published measurements, not reproduced here. Profile UIR on named Go modules before choosing caches, embeddings, or a new wire format. [Performance][g-performance] · [Response catalog][g-features-runtime]
- **arch-unit migration:** the inspected arch-unit worktree aliases canonical UIR types but retains older AST query and plugin seams, and its UIR rule analyzer is still a stub. Integration should use UIR's current typed query semantics deliberately, after the alias migration stabilizes. [Alias][a-alias] · [Legacy query parser][a-query] · [Rule analyzer][a-rules] · [UIR query][u-query]

## Verification plan for any follow-up implementation

1. Fix a small corpus containing two same-named methods in different packages, an unresolved call, an interface dispatch, a syntax-only package, two checkout heads, and a changed declaration. Preserve expected canonical IDs and source positions in golden fixtures.
2. For each query, assert proven rows, possible-candidate counts, effective scope, coverage, and completeness together. Include a bounded traversal that visibly truncates.
3. Run the same symbol query against both heads and one historical snapshot; run the diff between two named commits. An overlay test should change one file in a multi-file Go package and demonstrate that base snapshot results remain unchanged.
4. For broader search or agent-context work, use tasks from architecture explanation, bug localization, change impact, and contract discovery; record answer quality, latency, and bytes/tokens against the current UIR query path. [Gortex evaluation design][g-evaluation]

## Source references

[g-architecture]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/architecture.md#L18-L60
[g-performance]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/architecture.md#L64-L74
[g-features-graph]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/features.md#L17-L50
[g-features-search]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/features.md#L52-L75
[g-features-analysis]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/features.md#L67-L75
[g-features-workflow]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/features.md#L77-L95
[g-features-runtime]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/features.md#L97-L160
[g-edge]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/graph/edge.go#L563-L598
[g-name-only]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/query/name_only.go#L44-L58
[g-impact]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/analysis/impact.go#L37-L74
[g-overlay]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/graph/overlay.go#L32-L67
[g-overlay-view]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/graph/overlay.go#L317-L349
[g-overlay-drift]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/daemon/overlay.go#L503-L514
[g-contracts]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/contracts.md#L3-L25
[g-contract-bridge]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/indexer/contract_bridge.go#L55-L92
[g-checkouts]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/multi-repo.md#L149-L179
[g-multi-repo]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/multi-repo.md#L369-L408
[g-search]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/semantic-search.md#L1-L73
[g-search-docs]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/semantic-search.md#L70-L73
[g-features-contract]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/features.md#L120-L130
[g-features-quality]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/features.md#L109-L118
[g-graph-query]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/mcp/tools_graph_query.go#L111-L146
[g-closure]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/mcp/tools_closure.go#L21-L65
[g-budget]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/respbudget/respbudget.go#L159-L190
[g-server]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/server.md#L1-L60
[g-parity]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/internal/eval/parity/fence_test.go#L5-L60
[g-evaluation]: https://github.com/zzet/gortex/blob/33107203de4900f1229a3eeff631ff9802c6725a/docs/04-evaluation/methodology.md#L8-L20
[u-symbols]: symbols.md
[u-indexing]: module-indexing.md
[u-query]: query.md
[u-serve]: serve.md
[u-occurrence]: ../storage/document.go#L67-L80
[u-query-result]: ../query/modules.go#L51-L60
[u-coverage]: ../query/module_coverage.go#L17-L29
[u-diff]: ../symboldiff/diff.go#L26-L46
[u-node]: ../node.go#L128-L146
[u-statement]: ../statement.go#L9-L25
[u-endpoint]: ../endpoint.go#L40-L60
[u-agents]: ../AGENTS.md
[a-extractors]: https://github.com/flanksource/arch-unit/blob/cdcc186ccbe03be64983cb77f9ca8d6b4323cf15/analysis/all/all.go#L11-L25
[a-interface]: https://github.com/flanksource/arch-unit/blob/cdcc186ccbe03be64983cb77f9ca8d6b4323cf15/analysis/interface.go#L16-L23
[a-plugin]: ../../arch-unit/plugin/proto/plugin.proto#L9-L44
[a-alias]: ../../arch-unit/models/uir/doc.go#L1-L8
[a-query]: https://github.com/flanksource/arch-unit/blob/cdcc186ccbe03be64983cb77f9ca8d6b4323cf15/ast/query/query_parser.go#L162-L201
[a-rules]: https://github.com/flanksource/arch-unit/blob/cdcc186ccbe03be64983cb77f9ca8d6b4323cf15/analysis/rule_violation_analyzer.go#L8-L25
[a-k8s]: https://github.com/flanksource/arch-unit/blob/cdcc186ccbe03be64983cb77f9ca8d6b4323cf15/plugins/k8s/extractor.go#L27-L64
