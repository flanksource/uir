# Diffing two commits

`uir diff` lists the symbols a module root added, removed, and changed between two Git commits, and with `--stat` how many lines each symbol gained and lost. It reads the two commits' snapshots from the index and, for line counts only, the two commits' blobs from a registered checkout. With `--auto-index`, missing clean snapshots are generated from temporary Git worktrees without changing the registered checkout or its head snapshot. The design is in [symbol-index-storage.md](symbol-index-storage.md#diffing-two-commits); the implementation is the `symboldiff` package.

## Command

```sh
uir diff <from>..<to> --root <module> [--visibility exported|internal|all] [--stat] [--auto-index] [--include-tests] [--snapshot-from <uuid>] [--snapshot-to <uuid>]
```

| Flag | Meaning |
| --- | --- |
| `<from>..<to>` | Two revisions. A full 40- or 64-character commit hash is used as is; anything else (`HEAD~1`, a branch, an abbreviated hash) is resolved with `git rev-parse --verify <rev>^{commit}` in the root's registered checkouts, primary first. Both sides must be non-empty. |
| `--root` | The module path (`root_key`) to diff. Required. |
| `--visibility` | `exported` (default), `internal`, or `all`. A row is kept when the symbol's visibility on either side matches. |
| `--stat` | Adds per-package, per-file, and per-symbol `+added -removed` counts. Requires readable Git blobs. |
| `--snapshot-from`, `--snapshot-to` | Use this snapshot for that side instead of the newest clean one. The snapshot must belong to the root and record that commit as its revision. |
| `--auto-index` | Select or generate a clean snapshot for each commit under the current standalone module configuration. Historical indexing reads the commit's `go.mod` and `go.sum` with `GOWORK=off`, so the current workspace cannot replace dependencies or hide the historical module. It leaves registered checkout files and heads untouched. |
| `--include-tests` | Include Go test symbols in snapshots generated with `--auto-index`. Without it, test files are excluded, matching the usual indexing default. |

The command is a Clicky operation, so `--format json`, `--format yaml`, `--format markdown`, and `--format html` all render the same typed result, and `uir serve` exposes it as `POST /api/v1/modules/diff` with a body such as `{"args": ["<from>..<to>"], "root": "example.org/service", "visibility": "all", "stat": true, "auto-index": true}`. `uir history --root <module> [--location <checkout>]` and `GET /api/v1/modules/history` list local branches, recent commits, and up to 50 pull request refs from `origin` for the browser's History view. A `pr:<number>` revision fetches that pull request head from `origin` when selected.

To inspect one Git commit as change history, use `uir history show <commit> --root <module>`. It compares that commit with its first parent, indexes either missing clean snapshot on demand, and shows all symbol changes with line counts by default. `--visibility exported|internal|all`, `--include-tests`, and `--stat=false` control the result. An initial commit has no parent and returns an error; use `uir diff` with an explicit range when comparing other revisions.

```sh
uir history show HEAD --root example.org/ledger
```

```sh
uir diff HEAD~1..HEAD --root example.org/ledger --visibility all --stat
```

## Selecting the snapshots

For each commit the diff takes the root's snapshots whose `revision` equals the commit (through the `(root_id, revision)` index) and picks the newest whose `worktree_state` is `clean`. Indexing records `clean` only when `git status --porcelain` under the module is empty and every indexed file is tracked by Git; a snapshot that indexed an untracked or Git-ignored `.go` file is `dirty`, because the commit does not contain that file and the diff would otherwise report it as added or removed.

- No snapshot for the commit: without `--auto-index`, the diff fails and says the commit must be indexed first. With `--auto-index`, it creates a clean standalone snapshot from a temporary worktree.
- Only `dirty` snapshots: without `--auto-index`, the diff fails and names them, because a dirty snapshot's bytes may not be the commit's bytes. With `--auto-index`, it creates a clean standalone snapshot. Pass `--snapshot-from` or `--snapshot-to` to use a dirty snapshot explicitly; `--stat` then verifies each file's blob against the snapshot and reports the files whose bytes differ.
- Different `configuration_hash` on the two selected snapshots (another indexer version, `GOOS`, `GOARCH`, `CGO_ENABLED`, toolchain, or tags): the diff fails, because shape and body hashes are only comparable under one configuration. `--auto-index` selects or generates both sides under the current standalone configuration.

## Changed files

Both snapshots' active documents are derived with `storage.ActiveDocuments` without their content: changed-file detection compares document headers only (id, ordinal, coverage, revision). A path whose document id is equal on both sides is skipped without being read. A path present on one side only is an added or removed file. A path whose document changed only because a sibling in its package changed (same bytes, new input hash) is a changed file, but its symbols' fingerprints compare equal, so without `--stat` it is omitted without its document being decoded.

## Classification

Symbols are matched across the whole diff by identity, so a symbol that moved to another file is still one symbol. For changed `indexed` documents outside a package that is `syntax` or `partial` on either side, classification reads no document: both snapshots' effective symbols (`storage.EffectiveSymbols`, from the [symbol deltas](symbol-index-storage.md#symbol-membership-which-symbols-a-snapshot-defines)) give each defined symbol's shape and body fingerprints, the first eight bytes of its `shape_hash` and `body_hash`, and the `definition` postings of each side's changed documents say which of them define each symbol. A symbol declared once on each side it appears on is classified by its handle:

| Class | Condition |
| --- | --- |
| `added` | only on the new side |
| `removed` | only on the old side |
| `signature` | both sides, shape fingerprint differs |
| `body` | shape fingerprint equal, body fingerprint differs |
| `moved` | fingerprints equal, defining `path_key` differs |
| omitted | fingerprints and path equal |

A package with a `syntax` or `partial` changed document on either side, and a symbol declared more than once on a side (every `func init` of a package shares one canonical id), are matched over their decoded documents instead: first by canonical symbol id, then by `key` wherever one side has no id (a `syntax` document, or an unproven declaration in a `partial` document). Those rows use the same classes, compare the full `shape_hash` and `body_hash`, and treat `shape_hash` as not comparable when either side has no id.

### Which documents are read

A document is decoded only when the output needs it. With `--stat`, every changed document is read, for its symbol extents. Otherwise the diff reads the documents of `syntax`, `partial`, and `excluded` files, both sides of a symbol declared more than once, and, for a row the visibility filter shows, the new side of an `added` symbol, the old side of a `removed` one, and both sides of a `signature` change, whose shapes the row prints. A shown `body` or `moved` row prints no shape and is named from the `symbols` row and its owners' names. The chosen documents' content is loaded in one batched read (`storage.DocumentContents`).

`body_hash` digests the declaration's tokens without comments or whitespace, so a comment added inside a function leaves every hash equal. With `--stat`, such a symbol whose lines did change is reported as `body` with the note `comments or layout only; body_hash unchanged`, so the file's totals stay the sum of its rows. Without `--stat` it is not reported.

A rename is `removed` plus `added`, because the name is part of the identity. A parameter-type change is too, because parameter types are part of the identity; when exactly one removed and one added symbol share package, kind, owner, and name, both rows carry the same `group`, and the added row carries the old shape and a shape diff against it, so the change reads as one signature edit. The output never guesses other pairings.

A row has `kind`, `owner`, `name`, `visibility`, both shapes when they differ, both paths, line counts with `--stat`, and coverage markers. It is reported under its new file, or under its old file when removed; a `moved` row names the file it came from. Nested symbols (struct fields, interface methods) are rows of their own, but their lines belong to the outermost declaration that contains them, so they carry no line counts.

### Coverage

| Marker | Meaning |
| --- | --- |
| `partial` | The package type-checked with errors on either side. Only proven symbols have ids; unproven ones are matched by key, and a symbol missing from a partial side may be unproven rather than removed. |
| `syntax` | The package did not type-check on either side. Symbols are matched by key and can only be `added`, `removed`, `body`, or `moved`. |
| `excluded` | The file is not extracted (build constraints) on either side. All of its lines are file scope; symbols the other side declares are reported without line counts. |
| `manifest` | `go.mod` or `go.sum` differs between the two commits (with `--stat` only). It has no rows; its whole diff is file scope. |

## Line attribution

With `--stat`, each changed file's bytes are read at both commits with `git cat-file -p <commit>:./<path>` in a registered checkout of the root that contains the commit (the primary location first, then any other location where `git rev-parse --verify <commit>^{commit}` succeeds). Each blob is hashed with SHA-256, as the indexer hashes source files, and must equal the source revision's `content_hash`.

Each side of a file is cut into segments: one per outermost symbol extent (widened to whole lines when nothing else shares them), plus a single `(file scope)` segment of everything between them: the package clause, imports, free comments, and blank lines. Segments are paired by symbol id (or key), the two file-scope segments are paired with each other, and each pair is line-diffed with a Myers diff. A symbol on one side only counts all of its lines as added or removed. A moved symbol is diffed against its old extent and counted under its new file.

A file's total is the sum of every row reported under it, hidden rows included, plus its file scope; a package's total is the sum of its files. For a file without moved symbols this is normally what `git diff --numstat` reports; the two can differ when Git's whole-file diff aligns lines across a declaration boundary, which a per-segment diff never does.

## Rendering

```text
example.org/ledger/money  +11 -6
  money/money.go (modified)  +11 -6
    Amount               signature +1 -1
        type Amount struct {
        	Cents    int64
      - 	Currency string
      + 	Currency Code
        }
    Half                 removed   +0 -1  func Half(a Amount) Amount
    Half                 added     +1 -0
      - func Half(a Amount) Amount
      + func Half(a *Amount) Amount
    Scale                signature +2 -2
      - func Scale(a Amount, factor int) Amount
      + func Scale(a Amount, by int) Amount
    (file scope)                   +2 -0
```

A signature row shows a shape diff. The two canonical shapes are line-diffed with Myers, treating lines that differ only in whitespace (gofmt realignment) as equal. A removed line immediately followed by an added line that shares at least one token is a replaced pair, and is token-diffed with `go/scanner` tokens; short equal runs between edits are folded into the edit so a changed tail reads as one replacement.

The plain form (non-TTY output, `String()`) prints unchanged lines with two spaces, removed lines with `- `, and added lines with `+ `. The rich forms print a replaced pair as one `~ ` line with token marks, through clicky `api.Text`: removed tokens red and struck through, added tokens green and bold. In a terminal that is ANSI; in markdown `func Scale(a Amount, ~~factor~~**by** int) Amount` (wrapped in colour spans unless colour is off); in HTML clicky's `<s>` and `<strong>`. JSON carries the structured form: `shape_diff` is a list of lines `{op, text, paired, tokens: [{op, text}]}`, where `op` is `equal`, `delete`, or `insert`, and a paired delete line's tokens are its equal and deleted runs while the paired insert line's are its equal and inserted runs.

`--visibility exported` hides internal rows but never their lines: a file with hidden rows says `(N hidden rows)` and its totals still include them, so the totals never silently add up to less than the file's diff.

## Failure modes

| Failure | Effect |
| --- | --- |
| Empty revision, malformed range, unknown `--visibility`, unknown root | The command fails. |
| Revision that no checkout resolves | The command fails; pass the full commit hash. |
| Commit with no snapshot, or only dirty snapshots | The command fails and names the commit or the dirty snapshots. |
| Override snapshot of another root or another revision | The command fails. |
| Different configuration hashes | The command fails. |
| A symbol that a changed document defines is not effective on exactly the sides whose changed documents define it, or a symbol whose effective fingerprints differ is declared by no changed document | The command fails naming the symbol handle, because the symbol deltas are inconsistent with the documents. |
| No registered checkout contains a commit (`--stat`) | The result's `lines_error` and every file's `lines_error` say so; symbol rows are still reported, and manifests are not compared. |
| Blob unreadable, or its hash differs from the source revision (`--stat`) | That file's `lines_error` states the path, commit, and both hashes; its rows are reported without counts, and every other file completes. A moved symbol whose other file failed fails its destination file's counts with the reason. |
| Manifest on a dirty snapshot (`--stat`) | The manifest's `lines_error` says its bytes cannot be verified. |

## Not implemented

- The package-level `export_shape_hash` pre-check that the design suggests for skipping packages under `--visibility exported` is not applied: skipping would hide exported `body` rows and line counts, so every changed package is read.

## Browser history

The History page compares any two selected branches, commits, or pull request heads. The Git log shows recent commits; selecting one compares it with its first parent and displays changed packages, files, symbol rows, signatures, and line counts. Comparison choices and the selected log commit are kept in the URL.
