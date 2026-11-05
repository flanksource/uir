# Incremental Go indexing

`uir add [path]` discovers and immediately indexes Go modules; `uir reindex [path]` refreshes an already registered module location. Both load packages with `go/packages`, use `go/types` for proven symbol identities and references, and retain syntax facts from `go/ast` when typed extraction is incomplete. They publish immutable, module-root snapshots. See [module-root indexing](module-indexing.md) for commands, scope flags, dependency indexing, and the storage hierarchy.

```sh
uir add .
uir add ./internal/service
uir reindex .
uir reindex ../service-feature --force
```

A path inside a package is resolved to its nearest enclosing `go.mod` and the entire module is scanned. Adding a directory discovers every visible nested module under it; nested module files are excluded from their parent's source set. In Git checkouts, discovery includes tracked and nonignored untracked files. Directory symlinks, hidden and underscore-prefixed directories, `vendor`, and `testdata` are not traversed. Tests are excluded unless `--include-tests` is set.

`uir add` stays within the requested directory. `uir add --all` also selects modules named by an enclosing `go.work` outside it. Local dependencies required by a selected module are indexed through dependency resolution in either case. A module that an enclosing `go.work` does not use is loaded with `GOWORK=off`.

For each module, indexing resolves its full `go.mod` module path as logical root key and canonicalizes the physical location. It records parent location and mount path for nested modules; `.gitmodules` distinguishes a declared submodule from other nested modules. A Git worktree's `.git` file is accepted as a repository marker. Multiple checkouts of the same module path share logical root identity but maintain separate heads.

## Snapshot lifecycle

1. Discover modules and included `.go` paths, capture Git revision and build configuration, and hash source bytes and dependency manifests.
2. Resolve dependencies and compare package inputs with the previous head, or the primary head for a new location. Reuse unchanged analysis; finish package loading and document extraction before opening the publication transaction.
3. Verify inputs have not changed, then publish source revisions, content-addressed documents, postings, coverage, dependency links, and source, document, and symbol deltas. Unchanged bindings inherit from the base snapshot; deleted paths receive tombstones.
4. Write the immutable snapshot and advance the location's head with a version-checked update in the same transaction. A publication error rolls back that transaction; dependencies already published in separate calls remain available.

An unchanged run reuses its head when revision, source and manifest hashes, configuration, package context, and dependency set still match. `--force` re-extracts and verifies documents and publishes a new snapshot. A revision-only snapshot can have no file delta. Readers start from a location head or an explicit snapshot and reconstruct effective sources, documents, and symbols by walking its base chain newest first. A snapshot exists only after publication commits; there is no building or failed snapshot state. No automatic snapshot compaction or retention policy exists.

## Extracted facts and coverage

Documents record declarations, signatures, visibility, source ranges, layered hashes, identifier occurrences, calls, writes, and embedding facts. Typed declarations receive canonical symbol IDs; local bindings and unresolved targets keep their positions without invented identities. File and callable metrics are stored alongside documents for [SQL queries](sql-query.md#stored-metrics). [Symbols and references](symbols.md) explains display, identity, and call locators.

| Coverage | Available facts |
| --- | --- |
| `indexed` | The package type-checked without diagnostics; declarations and references have proven identities. |
| `partial` | Type checking or an individual document failed; only proven typed facts are recorded, with diagnostics. |
| `syntax` | The package could not be type-checked, or syntax analysis was explicitly selected; AST declarations and call locators remain, without canonical IDs or postings. |
| `excluded` | Build constraints exclude the file under the recorded variant; an empty document records the reason. |

A file that cannot be parsed is published as an empty document with a `uir/index` warning; its package's other parseable files retain syntax facts and `uir/coverage` diagnostics. Load and type errors remain visible in CLI warnings and query coverage. Database failures, changed inputs, cross-document invariant failures, and recognized dependency-download or filesystem failures stop indexing instead of publishing misleading coverage.

Build selection uses the recorded `GOOS`, `GOARCH`, `CGO_ENABLED`, toolchain, and `GOFLAGS`. Lint analysis also accepts explicit build tags through `uir lint --tags`; `add` and `reindex` have no `--tags` flag. Dirty and non-Git snapshots store source blobs; clean snapshots can retrieve pinned Git blobs. The browser verifies every source's SHA-256 before displaying it.

Queries traverse recorded facts within selected indexed snapshots. Interface dispatch uses recorded implementation facts; dynamic calls and dependencies outside that scope are not whole-program proof. An ambiguous selector returns candidates rather than choosing a declaration arbitrarily, and an empty result under incomplete coverage is inconclusive.

Use `uir query 'service.Run <' --root example.org/service` to inspect references and callers, and `uir query 'service.Run :uses' --root example.org/service` for resolved symbols used by its own body. Unresolved calls remain available through outgoing call queries (`>`), while `:uses` requires resolved targets. See the [query guide](query.md) for grammar and scoping.

## Lint analysis

`uir lint` selects syntax or typed analysis from each rule's SQL requirements. Syntax analysis reads declarations, metrics, and build headers without loading imported packages; typed rules load the selected module's type information. Analysis heads are stored separately from normal index heads and do not advance them. `--no-index` requires matching, fresh analysis; `--snapshot` evaluates explicit historical facts. See [SQL architecture linting](lint.md) for configuration, build tags, and changed/staged sources.
