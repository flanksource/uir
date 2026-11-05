# Test runs, coverage and flakiness

uir records which tests ran against exactly which indexed snapshot, with what outcome and coverage, so it can tell a regression from a flaky test. It never runs tests, parses test runner output, or finds tests in source: a producer such as gavel reports what the runner ran.

```bash
uir add . --include-tests
go test -coverprofile=cover.out ./...   # through gavel, which writes results.json
uir tests import results.json --coverprofile cover.out
uir tests flaky --since 168h
uir query 'SELECT symbol.name, symbol.generation, symbol.test_coverage FROM func:* WHERE symbol.test_coverage < 0.5'
```

## Import contract

`uir tests import` reads version 1 of the producer-neutral contract (`testruns.Import`):

```json
{
  "version": 1,
  "runs": [{
    "producer": "gavel",
    "environment": {"GOOS": "linux", "GOARCH": "amd64", "go": "go1.27.1", "race": "true"},
    "started_at": "2026-11-04T10:00:00Z",
    "finished_at": "2026-11-04T10:01:00Z",
    "inputs_digest": "optional digest of non-Go inputs",
    "tests": [{
      "framework": "go test", "package": "example.com/store", "suite": [], "name": "TestSave",
      "file": "store_test.go", "line": 12,
      "attempts": [{"outcome": "fail", "duration_ms": 340, "failure": "..."}, {"outcome": "pass", "duration_ms": 310}],
      "coverage": []
    }],
    "coverage": [{"path": "store.go", "start_line": 4, "start_col": 2, "end_line": 5, "end_col": 1, "statements": 1, "count": 3}]
  }]
}
```

Fields follow gavel's `parsers.Test`. Outcomes are `pass`, `fail`, `skip`, `timeout` and `aborted`; `fail` and `timeout` count as failures. Paths are module-relative. `--coverprofile` attaches a Go coverage profile as the run's aggregate `coverage`, and requires an import with one run.

The import binds runs to the snapshot `indexer.CheckCurrent` proves the checkout (`--path`, default `.`) currently is, under the head's test setting. A dirty or unindexed checkout fails; there is no nearest-snapshot fallback. A run's id derives from its snapshot and content, so importing the same file twice records it once.

## Tables

[`24_tests.hcl`](../storage/migrations/24_tests.hcl) adds:

| Table | Holds |
| --- | --- |
| `test_cases` | One row per test ever reported for a root: framework, package, length-delimited suite path and name. `symbol_handle` links it to the declaration it is: a `go test` test to the func of its top-level name in its file, any other framework to the innermost proven declaration enclosing its reported line. A test whose file the snapshot did not index has no link. Pruning counts the link as a reference. |
| `test_runs` | One row per run: its snapshot (set to null when pruning deletes the snapshot), producer, environment and its digest. |
| `test_results` | One row per test, run and attempt, with the result's closure fingerprint and closure source. |
| `test_coverage` | Per run, or per test for per-test profiles, the statements and covered statements per document and proven declaration (`symbol_handle` 0 is file scope), with the declaration's `content_fp` and `content_generation` at the run's snapshot. Rows cascade with their document. |

## Coverage

Each coverage block is attributed to the innermost proven declaration whose extent lines contain it, or to file scope. Lines are compared, not columns, because profiles count columns in bytes and documents in UTF-16 units. A block naming a path the snapshot does not serve, or crossing a declaration boundary, fails the import with its position.

`symbol.test_coverage` is the share of a symbol's statements the newest run's aggregate coverage executed, and `symbol.test_coverage_generation` the generation it was measured at. Coverage is reused on a later snapshot only while the symbol's `content_fp` there equals the one it was measured at, so an unrelated edit keeps it and a body edit drops it to `NULL`. Neither field decodes documents.

## Flakiness

`closure_fingerprint` digests the source, the snapshot's dependency set hash, the run's `inputs_digest`, and the sorted `(canonical id, content_fp)` pairs of the symbols the test depends on. `closure_source` says how that set was found, without silent fallback between groups:

| Source | Symbols | When |
| --- | --- | --- |
| `coverage` | Declarations with covered statements in the test's own coverage, plus the `static` set | The test carried its own coverage and the `static` set is complete |
| `static` | The test symbol and the symbols its graph reaches inside the indexed roots through calls, variable reads and variable writes, to depth 8 and 1000 nodes | The test is linked and the graph is complete: no unresolved call, nothing cut by depth or limit |
| `snapshot` | The whole snapshot (content set and context hashes) | Otherwise |

`uir tests flaky` reports a test when results with the same closure source, closure fingerprint and environment both passed and failed, or when one run's attempts both failed and passed. Results across a changed closure or a different environment are never compared. It is computed from `test_results` on each call; nothing is stored.

## Limits

- **Non-Go inputs.** Embedded SQL, testdata, templates and configuration files are not symbols, so they are in no closure. A test that fails because such a file changed looks flaky unless the producer supplies `inputs_digest`.
- **Directive comments.** `//go:embed` and marker comments do not change `content_fp`.
- **Test code** is not instrumented by Go coverage, so a `coverage` closure always includes the test's `static` set; a test without one gets `snapshot`.
- **Ginkgo specs** are not symbols. A spec links only when a proven declaration encloses its line, and then its static graph is that declaration's; an unlinked spec falls back to `snapshot`.
- **Static graphs** follow a function a variable's value names, such as `map[string]func(){"x": Other}`, through the read of that variable. A function stored into a variable elsewhere, such as by a registration call, is reached only if the walk passes through the code that stores it.
- **Clean checkout.** Import needs the checkout to match its indexed head, indexed with `--include-tests`. Untracked files a producer writes into it, such as gavel's `.gavel/` and `.tmp/`, must be gitignored or written elsewhere; the error lists the paths Git reports.
- Only tests a run reported are known: a test that never ran is not in `test_cases`.
- Only aggregate coverage is collected by default. Per-test coverage needs one profile per test from the producer.
