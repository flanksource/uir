# Resolved variable types and the request-state lint rule

Status: implemented locally; canonical verification is UNVERIFIED with 7/11 checks passing. Tracked TODO: `3782669d-e80e-4b96-8fc5-9fab9407d1b4`. Scope: UIR capability and a runnable equivalent of OIPA's `noGlobalRequestState` rule. OIPA rollout follows full acceptance and a usable UIR release.

## Problem and source of truth

`/Users/moshe/work/om/oma/oipa-cli/lint/ruleguard/rules.go` rejects package-level variables of nine exact types: `context.Context`, and value/pointer forms of `config.Config`, `envctx.Runtime`, `oipactx.OipaContext`, and `gorm.DB`. Its message points to the variable name. OIPA enables it through gocritic/ruleguard in `.golangci.yml`.

UIR already distinguishes package variables from constants, fields, parameters, and local variables: `indexer/symbol_identity.go` only creates `var` identities when the object's parent is its package scope. `var:*` therefore supplies the right declaration set. However, `storage.DocumentSymbol` has no resolved variable-type fact, and SQL has no `symbol.type` field. Existing human-readable shapes use package names and are unsuitable for identity matching.

Reuse `indexer/symbol_shape.go:encodeType`: it already writes fully qualified named types, recursively resolves aliases, preserves pointer/composite structure, and rejects invalid types. Do not add a second type-string encoder, parse shape text, match import aliases, or approximate this rule with references to forbidden types.

## Public contract

Expose `symbol.type` as a catalog field of type `string`. In this increment it contains the canonical resolved type of a package-level variable, including inferred types. Non-variable symbols return SQL `NULL`; local variables, struct fields, and constants are outside this feature's selection and rule scope.

Canonical spellings reuse the existing encoder, for example `context.Context`, `*gorm.io/gorm.DB`, and `[]*gorm.io/gorm.DB`. Exact equality distinguishes these. Aliases resolve to their target identity, including aliases inside pointers and composites. Newly defined named types retain their own identity: `type Custom gorm.DB` does not become `gorm.io/gorm.DB`. A type implementing `context.Context` is not itself the named `context.Context` type.

The field is available in SELECT, WHERE, ORDER BY, the query catalog, API responses, and existing SQL completion. Keep `SELECT *` unchanged, as callable metrics already do; callers request the type explicitly. This adds no SQL operators or rule-language syntax.

## Implementation sequence

1. Add failing Ginkgo tests for canonical variable-type extraction, immutable publication and SQL evaluation before implementing those seams. Use the existing encoder's tests to establish alias, pointer and named-type semantics.
2. Add an optional `DeclaredType` string fact to `storage.DocumentSymbol`, populated by `documentBuilder.typedSymbol` for proven package-level `*types.Var` declarations using `encodeType(object.Type())`. Preserve name ranges and canonical identities. A proven variable with an unencodable type is an extraction error; existing partial/syntax extraction must remain explicitly incomplete.
3. Validate nonempty supplied type facts and their applicability to variable declarations during publication. Accept omitted facts in older documents and external publications, but never infer them from payload text. Bump `DocumentFormatVersion`, the Go `IndexerVersion`, and the external producer version where publication/rendering semantics change. Update format documentation, validators, fixtures and version-sensitive tests together. Preserve declaration fingerprints and semantic hashes; the new fact is derived metadata.
4. Persist types on immutable document declarations, not global `storage.Symbol` identities: the same global variable can change type between snapshots without changing its identity. Initially reuse the existing document reader and cache; no relational table or SQL schema migration is required for this JSON fact. Do not overload `declaration_metrics` with non-metric data.
5. Register `symbol.type` in `query/sql_rows.go`; add a requested-field predicate beside `needsMetrics` and a loader beside `sqlRecordMetrics`. Resolve the declaring document using the same snapshot/ambiguity rules as other declaration facts. Preserve the original use-site witness when a navigation result projects a variable's type. Ensure queries combining types and callable metrics cannot incorrectly take the document-free metric projection path.
6. Add preflight checks for type-dependent queries before row filtering can hide missing facts. Require complete typed coverage of target scopes; partial or syntax-only extraction must fail with package/snapshot diagnostics rather than produce a clean request-state verdict. Reading a variable's absent type fact in an old or externally published document fails with snapshot/path/declaration and reindex/republish guidance. Ordinary queries that do not request types continue to work. Never backfill historical documents during migrations or queries.
7. Add `lint/testdata/request-state/.uir.yaml` with the exact rule below and a readable fixture module covering its positive/negative cases. Document the feature in `docs/lint.md`, `docs/sql-query.md`, and `docs/symbol-index-storage.md`. Extend catalog completion and API contract tests without hardcoded duplicate field catalogs.
8. Add and run the executable acceptance fixture described below, then attach it to the tracked TODO. Run focused tests, CLI smoke checks, `make build`, `make lint`, and the repository Go suite. Report existing failures separately; do not suppress them or alter unrelated files.

## Equivalent UIR configuration

```yaml
version: 1
lint:
  include_tests: true
  rules:
    - id: no-global-request-state
      severity: error
      message: "package-level request state {{.name}} is shared across users; pass it explicitly or through context"
      query: |
        SELECT symbol.name AS name
        FROM var:*
        WHERE symbol.type = 'context.Context'
           OR symbol.type = 'github.com/oipa/cli/internal/config.Config'
           OR symbol.type = '*github.com/oipa/cli/internal/config.Config'
           OR symbol.type = 'github.com/oipa/cli/internal/envctx.Runtime'
           OR symbol.type = '*github.com/oipa/cli/internal/envctx.Runtime'
           OR symbol.type = 'github.com/oipa/cli/internal/oipa/oipactx.OipaContext'
           OR symbol.type = '*github.com/oipa/cli/internal/oipa/oipactx.OipaContext'
           OR symbol.type = 'gorm.io/gorm.DB'
           OR symbol.type = '*gorm.io/gorm.DB'
        ORDER BY path, line, name
```

This configuration is implemented in `lint/testdata/request-state/.uir.yaml`. `include_tests` must be aligned with the actual Ruleguard invocation when comparing results; the example intentionally includes test globals.

## Acceptance criteria

- All nine forbidden exact types are reported for exported and unexported package variables, with and without initializers, grouped declarations, multiple names, inferred return types, import aliases and type aliases.
- Findings identify each variable name at its own line/column, rather than the initializer or a referenced type's declaration.
- Locals, parameters, fields, constants, unrelated same-named types from another package, newly defined named types, slices/maps/wrappers and deeper pointers do not match. `*context.Context` also does not match the original rule.
- Canonical strings resolve aliases recursively but never erase distinct named types or use assignability as equality. Compare the fixture against the actual Ruleguard matcher; investigate discrepancies before removing the original rule.
- Type changes across snapshots return the correct historical and current values without changing symbol identity. Test identical identifiers across separate checkouts, ambiguous multi-snapshot selection, unchanged reindex, and immutable reuse conflicts.
- SQLite and PostgreSQL serve the same results. Missing historical/external facts and incomplete coverage produce actionable errors, including when no variable rows would otherwise survive a predicate. Excluded documents do not trigger unavailable-fact errors.
- `symbol.type` works in projection, predicates, ordering, catalog completion, API JSON and lint. Non-variable rows expose NULL. Reference navigation retains its source witness while loading the selected declaration's type.
- Existing `--rule`, `--include`, `--exclude`, `--no-index`, and `--snapshot` semantics remain intact. Excluding a source finding does not disable type or coverage validation.
- Queries read no live source files or source blobs to reconstruct types. Measure cold/warm time and document loads on an indexed corpus; cache documents within the existing pipeline. Keep metric-only queries on their existing document-free path. Add a relational projection only as a separate measured optimization if needed.
- No Ruleguard removal, lint suppression changes, commits, or publication occur as part of planning.

## Executable definition of done to add during implementation

Create `fixtures/resolved-variable-types.md` using Gavel's command table and CEL assertions. The document must run the following checks with nonzero-spec guards, realistic timeouts and isolated databases under `.tmp/`:

| Check | Required evidence |
| --- | --- |
| Focused Ginkgo specifications in storage, indexer and query | Extraction, validation, scope, history, missing facts and both database engines pass; output must contain executed passing specifications, never only skipped tests. |
| Lint parity fixture | Exact independently specified variable names, paths, lines and columns match all positive/negative cases. Compare against the original Ruleguard rule with a compatible runner and aligned build tags/test inclusion. |
| CLI JSON | `make binary`, then lint the example module with an isolated DSN; error findings exit 1, filtered findings behave correctly, and no-index/historical runs preserve witnesses. |
| Catalog and API | Vitest API tests and SQL completion tests prove discovery, request/response preservation and field completion. |
| I/O and performance | Record exact corpus/snapshot, command, cold/warm durations and document/source-read counters; assert zero source reconstruction and unchanged metric-only I/O behavior. |
| Repository gates | `make build`, `make lint`, `go test ./...`, and released-module compatibility checks; distinguish pre-existing blockers from feature regressions. |

Do not attach a future or unexecuted fixture as verified evidence. Use `gavel todos check` for the eventual canonical verdict. Build/lint were previously blocked by unrelated `hack/postdeploymentdiff` sqlglot API errors and Go 1.27 export-data incompatibility in the pinned linter; recheck at implementation time.

## OIPA rollout boundary

After UIR parity and acceptance are established and the binary is available through the normal release/install path, add this rule to OIPA's `.uir.yaml` and wire its existing lint workflow to UIR. Compare actual OIPA findings with Ruleguard before replacing that check. Remove the custom Ruleguard rule and its specific gocritic wiring only after parity is proven; remove the DSL dependency only if unused elsewhere. Preserve other lint configuration and dirty work. Do not commit local workspace replacements or bypass an unpublished dependency with a local-only distribution path.

OIPA mutations and release execution remain behind the release boundary above; its original Ruleguard configuration has not been changed.

## Implementation evidence

Document format 8, `go-types-v8` and `external-v5` record optional canonical variable types. SQL resolves `symbol.type` against the selected declaration and rejects incomplete coverage or unavailable facts before filtering. No database table was added, and existing metric-only I/O tests pass without document or source reads.

Focused SQLite/PostgreSQL type, publication and validation tests pass, together with API and completion tests. The CLI example reports 13 violations. The build-tagged `ruleguardparity` specification runs the copied original rule with Ruleguard v0.4.5 under Go 1.22.12 and checks that all 11 original findings have identical name positions and messages in UIR. UIR additionally catches both variables in a grouped declaration that the original AST pattern misses; it does not reproduce that omission.

The isolated seven-file query I/O fixture recorded 2.98 ms cold and 0.52 ms warm, one document-content query cold and none warm, and zero source-blob reads. These are fixture measurements, not real-corpus latency guarantees. Raw evidence is `.tmp/variable-types-focused.log`; acceptance is `fixtures/resolved-variable-types.md`.

`gavel todos check 3782669d --timeout 20m --max-concurrent 1 --max-retries 0 --no-progress --no-color` completed on 2026-10-19 with 7/11 checks passing. Type extraction/publication/validation/I/O, all lint specifications, direct Ruleguard parity, existing metric I/O, API types, completion, and CLI error findings all pass. Build, lint, the Go suite and released-module compatibility remain blocked by unrelated sqlglot API errors in `hack/postdeploymentdiff`, the pinned linter's Go export-data incompatibility, three existing graph nil/empty-slice assertions, and unpublished commons-db Git APIs. Generated publication examples were regenerated to format 8, and their consistency test passes. No suppression or unrelated implementation change was made.

The canonical run printed a UTF-8 error while persisting one Captain iteration record, but the TODO's UNVERIFIED state was read back successfully. Raw verifier output is `.tmp/variable-types-todo-check.log`; initial full-run output is `.tmp/variable-types-fixture.log`. OIPA's original rule remains installed pending the rollout boundary above.
