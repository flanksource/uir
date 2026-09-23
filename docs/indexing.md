# Incremental Go indexing

`uir project reindex` discovers Go sources, extracts syntax with the standard library `go/ast`, and publishes an immutable relational snapshot. The operation runs inside one context-bound Clicky task, so cancellation and task output follow the same lifecycle as the rest of the CLI.

```sh
go run ./cmd/uir --dsn ./state/uir.db project reindex billing --path ../billing --root app
```

The project is created on its first reindex. Later runs follow its current `ProjectHead`, compare root and file content hashes, and either return the existing snapshot unchanged or publish a new head.

The result includes `timings.discovery_ms`, `load_ms`, `preparation_ms`, and `publication_ms` so a slow run can be attributed to root scanning, previous-snapshot loading, AST or cached-projection preparation, or transactional publication. An unchanged run has zero preparation and publication time.

| Flag | Meaning |
| --- | --- |
| positional `project` | Stable logical project key. |
| `--path` | Workspace or repository directory. Defaults to the current directory. |
| `--root` | Stable key for the top-level root. Defaults to the indexed directory name. |
| `--name` | Optional project display name. It does not participate in identity. |
| `--include-tests` | Include `_test.go` files. Tests are excluded by default. |
| `--force` | Reparse every source and publish a new snapshot even when content is unchanged. |

## Snapshot lifecycle

An index run performs these steps:

1. Discover the top-level root and every nested Git boundary before scanning files.
2. Normalize root-relative source paths and hash every included Go file.
3. Compare `(root_key, path_key, content_hash)` with the published snapshot.
4. Parse changed and new files with `parser.ParseFile`; reconstruct unchanged file projections from relational nodes, fields, locations, and relationships.
5. Omit deleted sources instead of copying them into the new snapshot.
6. Write a complete `building` snapshot, including roots, sources, package/type/method/field nodes, locations, and call relationships.
7. Resolve call targets only when their structured identifier selects exactly one same-snapshot node in the allowed root scope.
8. Mark the snapshot `ready` and compare-and-swap `ProjectHead.Version` in the same transaction.

Readers following `ProjectHead` therefore see either the old complete snapshot or the new complete snapshot. A concurrent writer that moved the head causes the transaction to fail with a retryable error; the indexer never mutates the published graph in place.

An unchanged run does not create a snapshot. A changed run creates new rows even for unchanged files because snapshots are immutable, but it avoids reparsing those files: their validated relational projection is copied into the new snapshot. A change to extractor version or indexing configuration disables reuse and reparses every included source.

Reusable declarations, fields, and calls are loaded in source batches. Call payloads retain the original syntax target and root scope, so a reused call is resolved again against the new snapshot when roots or overloads change. Publication indexes exact and signature-free call targets once per snapshot, including root-scoped and cross-root lookup buckets; an ambiguous bucket stays unresolved. Sources, nodes, locations, fields, and relationships are inserted in bounded batches while preserving parent-before-child foreign-key order. The phase timings show the cost of this full publication separately from source preparation.

## Roots, repositories, and submodules

The filesystem path is not symbol identity. The indexer models the supplied directory as one root and treats every nested directory containing a `.git` directory or file as another root. Files in a nested root are excluded from the parent scan and indexed exactly once.

- The top root uses `--root` or the directory basename.
- A nested root key is `<top-root>/<mount-path>` and its `parent_root_id` points to the nearest enclosing root.
- A path declared in the nearest parent's `.gitmodules` is stored as `kind = "git-submodule"` with `submodule_path`; an undeclared nested checkout remains `kind = "git"` without claiming submodule provenance.
- Git worktrees are detected because a `.git` file is a root marker.
- Each root stores its own revision, remote URI when available, content-set hash, mount path, and local diagnostic path.
- The same `identity_key` may occur in several roots. `(root_id, identity_key)` is the enforced identity, and ambiguous cross-root call targets remain unresolved.

Hidden directories, underscore-prefixed directories, `testdata`, `vendor`, and `.git` metadata are skipped. Nested Git roots are detected before the parent source scan, so a discovered root is not flattened into its parent. Directory symlinks are not followed; a source tree that intentionally crosses a boundary should be supplied as its own explicit root rather than relying on machine-specific links.

## Extracted Go syntax

The Go extractor records:

- packages using the nearest `go.mod` module path plus the source directory;
- declared types and struct fields;
- functions and receiver methods, including formatted parameter and result signatures;
- public or private visibility from Go identifier casing;
- declaration line and column ranges;
- function and method call occurrences, including import-qualified package targets;
- lossless UIR node JSON, semantic hashes, field projections, and durable call locators.

Declarations and files are sorted before persistence, making source traversal and row ordinals deterministic. Receiver unwrapping supports pointer, parenthesized, and generic receiver forms, adapted from gopatch's syntax tree handling. File discovery follows gopatch's exclusion rules while adding explicit nested-root ownership.

## Syntax-only limits

The extractor intentionally uses syntax, not `go/packages` or `go/types`. This keeps reindexing fast and permits incomplete worktrees, but it imposes explicit limits:

- An import selector such as `helper.Notify` carries the import path and can resolve when that package has one matching declaration in the snapshot.
- An unqualified call is scoped to the current root and package.
- A receiver call such as `service.Run` has no statically known receiver type and remains unresolved. The indexer does not reinterpret it as a same-named package function.
- Built-ins, conversions, generated functions, dependencies outside the indexed roots, and ambiguous same-name methods remain unresolved call relationships.
- Build tags and platform-specific file selection are not evaluated; every included `.go` source is parsed. Identical declarations from platform variants share one node with multiple locations and source-specific call occurrences. Conflicting variants with the same identity but different semantic payloads fail loudly because the current storage identity has no build-constraint dimension. `_test.go` inclusion is controlled separately.
- The indexer does not type-check packages or report compilation validity. Syntax errors fail the run immediately and leave the published head unchanged.

Unresolved calls are first-class rows, not dropped errors. Query them with:

```sh
go run ./cmd/uir --dsn ./state/uir.db project query billing --expression 'unresolved calls where root = "app"'
```
