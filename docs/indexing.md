# Incremental Go AST indexing

`uir add [path]` discovers and immediately indexes Go modules; `uir reindex [path]` refreshes an already registered module location. Both use the same `go/ast` extractor and publish immutable, module-root snapshots. See [module-root indexing](module-indexing.md) for commands, scope flags, and the storage hierarchy.

```sh
uir add .
uir add ./internal/service --no-workspace-uses
uir reindex .
uir reindex ../service-feature --force
```

A path inside a package is resolved to its nearest enclosing `go.mod` and the entire module is scanned. Adding a directory discovers every nested module under it; nested module files are excluded from their parent's source set. Directory symlinks, hidden and underscore-prefixed directories, `vendor`, and `testdata` are not traversed. Tests are excluded unless `--include-tests` is set. An enclosing `go.work` can name module paths outside the selected directory; interactive add asks about those, while non-interactive add requires an explicit include/exclude flag. External uses are published in a separate index call.

For each module, indexing resolves its full `go.mod` module path as logical root key and canonicalizes the physical location. It records parent location and mount path for nested modules; `.gitmodules` distinguishes a declared submodule from other nested modules. A Git worktree's `.git` file is accepted as a repository marker. Multiple checkouts of the same module path share logical root identity but maintain separate heads.

## Snapshot lifecycle

1. Discover modules and included `.go` paths, capture Git revision where available, and hash source bytes.
2. Inside the publication transaction, compare each path with the effective source set of the location's previous head, or the primary head when a new location is first registered. Parse changed files there; reuse an immutable `SourceRevision` only when root, path, content hash, package path, and extractor version agree.
3. Write `set` deltas for changed/new paths and `delete` tombstones for removed paths. Unchanged paths inherit from the base snapshot without another row.
4. Mark the snapshot ready and advance that location's head with a version-checked update in the same transaction. An error rolls back every module in that `IndexModules` call.

An unchanged run reuses its head unless `--force` requests publication or the Git revision changed. A revision-only snapshot has no file delta. Readers start from a ready head or an explicit ready snapshot and reconstruct effective sources by walking its base chain newest first. The indexer does not mutate a published projection in place. No automatic snapshot compaction or retention policy exists.

## Extracted syntax and limits

The extractor records packages, declared types and struct fields, functions and receiver methods with signatures, visibility, declaration positions, UIR payloads and semantic hashes, and call occurrences with structured target identifiers. Receiver unwrapping covers pointer, parenthesized, and generic forms. Source revisions store this projection as JSON; there are no per-node GORM tables. [Symbols and references](symbols.md) explains display, identity, and call locators.

This is syntax indexing, not a compiler or whole-program call graph:

- Import-qualified selectors retain the import path and can resolve against a matching indexed declaration; unqualified calls use current package context.
- A receiver expression such as `service.Run()` has no proven static receiver type and may remain unresolved. Built-ins, conversions, generated functions, and dependencies outside indexed modules may also remain unresolved.
- Build tags and platform-specific file selection are not evaluated. All included Go files are parsed; `_test.go` inclusion is separate. Syntax errors abort publication and leave heads unchanged.
- A call can be ambiguous across modules or checkout heads. Graph query target selection returns candidates instead of choosing arbitrarily; a signature-free locator can match a declaration signature, but that is not type-checked proof.
- Source bytes are not persisted. The browser only shows a local or pinned Git file whose hash matches the indexed revision; an uncommitted historical version may be unavailable once the working tree changes.

Use `uir query 'service.Run <' --root example.org/service` to inspect indexed callers. Unresolved call locators remain in stored documents but have no canonical edge in the compact query graph. See the [query guide](query.md) for grammar and scoping.
