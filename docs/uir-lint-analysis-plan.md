# Module-scoped lint analysis

Status: implemented and verified. Gavel TODO `329053bd-5e67-48ed-87c2-e82775ac9778` passed all six definition-of-done checks on 2026-10-21. Scope: syntax-only metric/header rules, current-module typed analysis, and explicit multi-module queries through `uir lint --all`.

## SQL requirements and execution

Infer analysis requirements from parsed SQL after rule selection and exclusions. File metrics, source headers and direct callable metrics need syntax. Type fields, identities, references, relationships, nested navigation and membership queries need typed facts. Inspect projections, predicates and ordering, including aliases. Keep YAML unchanged.

Execute syntax rules first and retain their findings if typed analysis fails. Restore configured rule order in the flat report. Verify that both modes used the same project sources and revision. Explicit syntax snapshots reject typed queries. Intentional syntax coverage does not generate an incomplete-coverage warning; genuinely incomplete typed coverage still does.

## Extraction and persistence

Add an analysis entrypoint beside ordinary module indexing. Syntax extraction reads the selected module's source through `indexer/syntax`, honoring test selection, filename constraints, build tags and cgo selection. It never calls the Go package loader. Typed analysis uses the existing loader to resolve imported types but publishes documents only for the selected module. Neither path recursively registers or indexes dependency modules.

Extend the shared syntax extractor with named and anonymous interface signatures so callable metrics retain parity with typed queries, while file function counts still exclude interface signatures. Bump the indexer version for the extraction change.

Reuse immutable snapshot publication and source blobs. Record syntax/typed mode on snapshots and store separate analysis heads keyed by location, mode and build configuration. Keep ordinary location heads and dependency indexing unchanged. Preserve analysis heads during history pruning without consuming the ordinary snapshot retention quota. Exclude analysis snapshots from ordinary version-dependency reuse.

Include analysis heads in the derived-table cutover sequence before dropping snapshots, and verify migration behavior on both database backends.

Validate source bytes, module/workspace manifests, build configuration, test selection and indexer version before reuse. Typed analysis additionally fingerprints imported local package directories, including new files; unrelated workspace package source does not invalidate it. Fail stale or missing `--no-index` requests explicitly. Recheck inputs at publication and use optimistic concurrency for head updates.

## Module selection and source modes

Default to the enclosing Go module at the invocation directory, or the explicit configuration's directory. Stop at nested module boundaries. Fail when no enclosing module exists.

`--all` selects registered modules' primary locations, uses the selected lint configuration, and evaluates rules over one combined pinned snapshot scope. External producers use their saved heads. Reject `--all` with a historical snapshot or Git change selection. File filters apply to reported findings, with paths relative to each selected module under `--all`.

Keep staged and historical source preparation in `commons-db/git`. Use captured configuration, manifests and bytes, retain original checkout paths, and avoid advancing live analysis heads. Typed captured sources use isolated manifests with workspace mode disabled and mutable local replacements removed.

## Witnesses, documentation and verification

Use module/file/declaration-span witnesses for syntax grouping without inventing canonical symbol IDs. Attach the same witness to typed rows so findings for one declaration combine across modes. Preserve row positions, severity and exit behavior.

Document the selection and reuse contract, expose snapshot analysis mode through the existing API model, and cover its response shape in the API tests. Extend Ginkgo tests for extraction, unavailable imports, reuse, current-module scope, combined cross-module traversal, grouping, source modes, pruning and normal-head preservation on SQLite and PostgreSQL.

Run the focused [acceptance fixture](../fixtures/uir-lint-analysis.md), `make build`, `make lint` and `go test ./...`. Record unrelated environment or dependency failures separately. Do not commit or change project lint suppression settings.

## Verification result

`gavel todos check 329053bd --timeout 30m --max-retries 0 --no-progress --no-color` passed 6/6 checks. `make build`, the API SQL tests, browser type checking, and scoped Go lint with golangci-lint v2.13.1 passed. The pinned v2.6.2 analyzer cannot decode Go 1.27 export data. The broad Go run retains three existing graph nil/empty-slice assertion failures and reached its 10-minute indexer limit during ordinary run-memo tests; the focused run-memo checks passed in acceptance. The new analysis-head migration cutover failures found in that broad run were fixed and verified on SQLite and PostgreSQL. Standalone release validation still requires the previously unpublished commons-db Git APIs documented in `lint.md`.
