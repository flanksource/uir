# Diffing two commits

`uir diff` lists the symbols a module root added, removed, and changed between two Git commits, and with `--stat` how many lines each symbol gained and lost. It reads the two commits' snapshots from the index and, for line counts only, the two commits' blobs from a registered checkout. Nothing is stored: both inputs are immutable, so the same command always prints the same result. The design is in [symbol-index-storage.md](symbol-index-storage.md#diffing-two-commits); the implementation is the `symboldiff` package.

## Command

```sh
uir diff <from>..<to> --root <module> [--visibility exported|internal|all] [--stat] [--snapshot-from <uuid>] [--snapshot-to <uuid>]
```

| Flag | Meaning |
| --- | --- |
| `<from>..<to>` | Two revisions. A full 40- or 64-character commit hash is used as is; anything else (`HEAD~1`, a branch, an abbreviated hash) is resolved with `git rev-parse --verify <rev>^{commit}` in the root's registered checkouts, primary first. Both sides must be non-empty. |
| `--root` | The module path (`root_key`) to diff. Required. |
| `--visibility` | `exported` (default), `internal`, or `all`. A row is kept when the symbol's visibility on either side matches. |
| `--stat` | Adds per-package, per-file, and per-symbol `+added -removed` counts. Requires readable Git blobs. |
| `--snapshot-from`, `--snapshot-to` | Use this snapshot for that side instead of the newest clean one. The snapshot must belong to the root and record that commit as its revision. |

The command is a Clicky operation, so `--format json`, `--format yaml`, `--format markdown`, and `--format html` all render the same typed result, and `uir serve` exposes it as `POST /api/v1/modules/diff` with a body such as `{"args": ["<from>..<to>"], "root": "example.org/service", "visibility": "all", "stat": true}`.

```sh
uir diff HEAD~1..HEAD --root example.org/ledger --visibility all --stat
```

## Selecting the snapshots

For each commit the diff takes the root's snapshots whose `revision` equals the commit (through the `(root_id, revision)` index) and picks the newest whose `worktree_state` is `clean`. Indexing records `clean` only when `git status --porcelain` under the module is empty and every indexed file is tracked by Git; a snapshot that indexed an untracked or Git-ignored `.go` file is `dirty`, because the commit does not contain that file and the diff would otherwise report it as added or removed.

- No snapshot for the commit: the diff fails and says the commit must be indexed first. Check the commit out in a registered location and run `uir reindex`.
- Only `dirty` snapshots: the diff fails and names them, because a dirty snapshot's bytes may not be the commit's bytes. Pass `--snapshot-from` or `--snapshot-to` to use one anyway; `--stat` then verifies each file's blob against the snapshot and reports the files whose bytes differ.
- Different `configuration_hash` on the two snapshots (another indexer version, `GOOS`, `GOARCH`, `CGO_ENABLED`, toolchain, or tags): the diff fails, because shape and body hashes are only comparable under one configuration.

## Changed files

Both snapshots' active documents are derived with `storage.ActiveDocuments`. A path whose document id is equal on both sides is skipped without being read. A path present on one side only is an added or removed file. A path whose document changed only because a sibling in its package changed (same bytes, new input hash) is read, its symbols compare equal, and the file is omitted.

## Classification

Every symbol entry of every changed file, on both sides, is matched across the whole diff: first by canonical symbol id, so a symbol that moved to another file is still one symbol, then by `key` wherever one side has no id (a `syntax` document, or an unproven declaration in a `partial` document).

| Class | Condition |
| --- | --- |
| `added` | only on the new side |
| `removed` | only on the old side |
| `signature` | both sides have an id and `shape_hash` differs |
| `body` | `shape_hash` equal (or not comparable), `body_hash` differs |
| `moved` | hashes equal, `path_key` differs |
| omitted | hashes and path equal |

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
| No registered checkout contains a commit (`--stat`) | The result's `lines_error` and every file's `lines_error` say so; symbol rows are still reported, and manifests are not compared. |
| Blob unreadable, or its hash differs from the source revision (`--stat`) | That file's `lines_error` states the path, commit, and both hashes; its rows are reported without counts, and every other file completes. A moved symbol whose other file failed fails its destination file's counts with the reason. |
| Manifest on a dirty snapshot (`--stat`) | The manifest's `lines_error` says its bytes cannot be verified. |

## Not implemented

- The package-level `export_shape_hash` pre-check that the design suggests for skipping packages under `--visibility exported` is not applied: skipping would hide exported `body` rows and line counts, so every changed package is read.
- Indexing an arbitrary commit through a temporary worktree is a separate command.

## Not yet built

- The explorer diff view from step 7 of the [symbol index design](symbol-index-storage.md) does not exist; the web UI has no page for this command's result. It is tracked follow-up work; until then use the CLI or `POST /api/v1/modules/diff`.
