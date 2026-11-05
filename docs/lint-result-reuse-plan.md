# Lint findings as generation-gated facts

Status: phases 1–3 (storage, closures, reuse in `uir lint`) are implemented; see [Result reuse](lint.md#result-reuse). Phases 4 and 5 belong to gavel and oipa-cli. It builds on [Symbol generations, test coverage and test flakiness](symbol-generations-tests-plan.md) and reuses its `content_generation` and closure-fingerprint design rather than adding a second one.

Where the implementation differs from this plan: closures are proven per subject only for symbol selector sources reading the subject's own facts (and per file for `files()` and syntax analysis, which has no canonical symbol ids); navigation and membership rules fall back to one closure over the whole scope, since their rows read symbols they are not about. A row is keyed by (rule, subject) and replaced when re-evaluated, with the rule version and closure fingerprint as columns, rather than keeping a row per fingerprint.

## Problem

`uir lint` reuses the index: a repeat over an unchanged checkout skips re-indexing (`lint/dirty_reuse_test.go`). It still evaluates every selected rule over the whole snapshot (`lint.Run` → `execute`, `lint/run.go`). Linters outside UIR that want to skip unchanged work each build a cache with its own key and staleness rule:
- gavel's old time-based debounce;
- oipa-cli's per-entity lint cache, keyed on its pre-lowering input digest (`fp:build`).

UIR already knows what changed: symbols carry layered fingerprints (`public_fp`, `private_fp`, `content_fp`; `fingerprint_layers.go`), and the database catalog lowers tables, columns and indexes into nodes with the same layers. Adding an index is a `private` change of its table, and a row-count change is no change. Linting can reuse results on exactly that basis, for every linter UIR indexes, instead of each linter guessing.

## Model

A finding is a derived fact. Its key:

| Part | Meaning |
| --- | --- |
| `rule_id`, `rule_version` | The rule, and a hash of its compiled query and message template, so editing a rule invalidates only that rule. |
| `subject` | The symbol handle the finding is about (the row's source location resolves to it). A rule that reports nothing for a subject stores an explicit empty result, so "clean" is reused too. |
| `closure_fingerprint` | A canonical hash (`internal/canonicalhash`, versioned and length-delimited) of the sorted `(symbol handle → layer fingerprint)` pairs the rule read while evaluating the subject. |

**What the closure contains:**
- The query pipeline records every symbol it resolves for a subject: definitions, callers, callees, reads, writes and implementations navigated through. A rule never lists its inputs by hand.
- Each member contributes the layer the rule depends on. Code symbols contribute `content_fp`. Database objects contribute by how they are read:
  - a table read for its columns contributes its `public_fp`;
  - a rule that judges indexes, statistics or plans declares `reads: private` and gets the table's `private_fp`.
- The flakiness design uses `content_fp` rather than `content_generation` for the same reason it applies here: generations are chain-local, and two branches can reach the same content.

## Storage and reuse

- **Storage:** `lint_findings` sits beside `test_coverage`, with columns `(rule_id, rule_version, subject_handle, closure_fingerprint, content_generation, finding jsonb, produced_at)`. It is indexed on `(rule_id, subject_handle)`.
- **Reuse:** a row is reused in a later snapshot while the subject's recorded closure still hashes to the same `closure_fingerprint` there. `content_generation` lets a read report which generation it used, as `symbol.test_coverage_generation` does, so a stale finding is visible and never silently reused.
- **Evaluation:** `uir lint` evaluates only the `(rule, subject)` pairs whose closure changed, plus subjects new to the snapshot, and writes their rows. Everything else is read.
- **Reporting:** `lint.Run` already returns a gavel `linters.LinterResult`. It sets `cache: {source: "uir", fingerprint, stored_at, hit}`, with `hit` true only when every pair was reused and `stored_at` the oldest reused row.

## Boundary: live inputs

Facts outside UIR are in no closure: table row counts, execution plans, statement timings, activity or other application data. A producer whose findings depend on them owns their expiry. It stores an expiry with the finding, and an expired finding is re-evaluated even when its closure is unchanged.

oipa-cli's `--eval` findings are the reference case. Their expiry grows with how long the transaction's inputs have been unchanged, between 15 minutes and 7 days (`lint.cache.evalAgeFactor`, `lint.cache.evalTTL.min`, `lint.cache.evalTTL.max`).

## Consumers

- **`uir lint`:** incremental rule evaluation as above.
- **gavel:** shows `cache.source: "uir"` results as cached, and passes `--no-lint-cache` through as a full re-evaluation that overwrites rows. Gavel itself keeps no cache (see gavel's *Cached lint results*).
- **oipa-cli:** OIPA rules are already lowered into UIR (`plugins/oipa/uirindex`). Its per-entity cache moves its fingerprint from `fp:build` (the transaction's XML and copybook include closure, before lowering) to `fp:uir` (the subject's closure). The switch is a deliberate one-time miss: the key names its fingerprint source so it cannot collide.

## Effect

- An edit to a copybook re-lints only transactions whose closure includes it, including transitive includes. That is what `fp:build` already does for copybooks; `fp:uir` also narrows it to what each rule actually read.
- Adding an index re-evaluates only findings with `reads: private` on readers of that table.
- A DDL change to a table nobody reads re-lints nothing. Today oipa-cli's `schema:<digest>` invalidates every SQL check on any DDL change.

## Phases

1. **Storage.** `lint_findings` and its migration, reusing the `content_generation` column from the generations plan (its phase 1 is a prerequisite).
   - Tests: an empty result is stored and read back; a subject absent from the snapshot is not served.
2. **Closure recording.** The query pipeline (`query.NewPipeline`) records resolved handles per subject, and rules declare `reads: private` when needed.
   - Tests: a rule over a function records its callees; a rule joining a table records the table at the declared layer.
3. **Reuse in `uir lint`.** Evaluate only changed pairs; report `cache`.
   - Tests: an unchanged checkout evaluates nothing; a body edit re-evaluates rules whose closure holds that symbol; adding an index re-evaluates only `reads: private` rules.
4. **Gavel read path.** Gavel shows `source: "uir"`, and `--no-lint-cache` forces re-evaluation.
5. **oipa switch.** oipa-cli keys on `fp:uir`. Its live-input expiry is kept.

Tracked as TODO `20848c74`. Related: `8c74f281` (dependency-aware snapshot reuse) and `ce4ac707` (oipa batch entity lint).
