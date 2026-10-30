# Require hack build constraints

Status: implemented locally. Canonical acceptance is UNVERIFIED with 8/11 checks passing. Tracked TODO: `db2f8ec2-d5c1-4016-939d-aaf8977673ee`.

## Behavior

Add a warning rule named `hack-build-tag` to `.uir.example`. Select indexed Go files under root or nested `hack/` directories using existing module-relative paths. Test files participate when `lint.include_tests` is enabled. Similar directory names do not match. Report existing violations without editing their build constraints or commands.

```yaml
- id: hack-build-tag
  severity: warning
  message: "{{.path}} must require the hack build tag"
  query: |
    SELECT path
    FROM files(include_excluded => TRUE)
    WHERE path MATCH '**/hack/**/*.go'
      AND file.build_constraint NOT REQUIRES 'hack'
    ORDER BY path
```

The explicit modern `//go:build` expression must be satisfiable and impossible to satisfy with `hack=false`. Accept `hack`, `hack && linux`, `(hack && linux) || (hack && darwin)`, and `(hack || linux) && !linux`. Report absent/legacy-only headers, `ignore`, `!hack`, `hack || linux`, and contradictory constraints. Filename constraints do not satisfy this requirement.

## SQL interfaces

Add a Boolean `include_excluded` option to `files`. False remains the default. Allow option-only source argument lists while retaining positional-before-named ordering. Reject invalid option types, repeated options, unknown options, and positional arguments after options.

Expose `file.build_constraint` as a string containing the normalized modern expression without its directive prefix. Missing modern headers yield an empty string; non-Go sources yield NULL. Keep SELECT-star projections unchanged. Load the new field only when explicitly selected, filtered, or ordered.

Add `[NOT] REQUIRES 'tag'` restricted to this field, including file aliases. Validate a case-sensitive single positive Go tag. Preserve existing Boolean precedence and SQL NULL semantics. Discover fields referenced only by predicates and precompute requirement results before Boolean row evaluation.

Expose the field and option through the existing catalog and SQL completion, including recognition of `files(...)` with options.

## Storage and source execution

No migration, format change, indexer-version bump, or backfill is needed. Existing source revisions and excluded documents are retained by ActiveDocuments and the snapshot cache. Conditionally remove the query layer's CoverageExcluded filter; keep discovery, Gitignore, test settings, snapshots and symbol extraction unchanged.

Resolve snapshots, load cached metadata, apply the exclusion option, construct file records, and load only requested metrics. When build constraints are requested, group Go source cache misses per scope and recover bytes through readVerifiedSources. Accept checkout bytes only when hashes match; otherwise retain existing stored-blob/pinned-Git recovery and path containment checks. External publications require stored source blobs. Missing or corrupt bytes fail with path and snapshot context; lint adds rule context.

Keep a concurrency-safe 4,096-entry parsed-header LRU keyed by content hash in the shared pipeline cache. Store owned expression text and immutable syntax, not source buffers; reset with existing cache invalidation. Do not cache source-read failures. Reuse per-query Boolean analysis for repeated hashes.

Ordinary metric queries remain metadata-only. Cold constraint queries may read all selected Go source bytes before WHERE filtering; this version does not add filter pushdown. Warm queries reuse parsed headers. Measure counts and cold/warm latency before optimizing.

## Constraint analysis

Use a small standard-library-only internal/buildconstraint package. Recognize standalone modern directives in Go's leading comment header, handling license comments, block comments, BOM and LF/CRLF. Ignore directive-like text inside block comments, strings or source bodies. Parse with go/build/constraint. Duplicate and malformed recognized directives are execution errors. A modern directive controls when legacy directives also appear; this rule does not validate their consistency.

Build a reduced ordered Boolean decision diagram with deterministic lexical tag ordering, preserving repeated tags and negation. Treat tags independently of the host environment. An expression requires T only if it is satisfiable and unsatisfiable with T=false. Bound construction to 65,536 nonterminal nodes per expression and observe context cancellation; do not return approximate answers. Compile once per distinct expression within a query and reuse for all requested tags.

## Implementation and acceptance

Use TDD for pure analysis, grammar/binding, snapshot evaluation, and lint/completion integration. Preserve unrelated dirty files. Track executable acceptance in fixtures/hack-build-tag.md and attach it to a Gavel TODO; use todos check without starting an implementation run or committing.

Cover exact Boolean semantics, limits, cancellation, headers, invalid queries, aliases, NULLs, default membership, excluded/filename-excluded sources, historical source recovery, corrupt/unavailable source failures, cache reuse/eviction, and zero source reads for existing metric queries. Exercise SQLite and PostgreSQL. Verify exact lint paths/severity, rule selection, include/exclude and test settings, API response/catalog contracts, and completion.

Provide a self-contained fixture module and runnable commands. Keep malformed-header inputs in focused tests so the runnable module remains indexable. Update SQL/lint docs with snapshot source requirements and cold-query cost. Run focused Ginkgo/Vitest checks, a CLI smoke test with an isolated .tmp database, and make build, make lint, go test ./.... Report unrelated gate failures separately; never suppress them or mass-tag existing hack files. Verify completion in agent-browser when the managed local query UI is available. No commits or publication.

## Verification result

The canonical `gavel todos check db2f8ec2-d5c1-4016-939d-aaf8977673ee --timeout 20m --max-concurrent 1 --max-retries 0 --no-progress --no-color` completed with 8/11 checks passing. All eight feature checks pass: exact constraint analysis, SQL grammar, SQLite/PostgreSQL snapshot queries and cache/source recovery, unchanged metric IO, lint/CLI witnesses and filters, SQL completion, API serialization, and the native CLI's eight warning findings. Full acceptance remains unverified because the three required repository gates fail outside this change.

- `make build`: existing `hack/postdeploymentdiff` code references missing `sqlglot.ParseDiagnostic` and `sqlglot.ParseScript` APIs.
- `make lint`: the pinned linter cannot decode Go 1.27 standard-library export data (version 4, maximum supported version 2).
- `go test ./...`: the same hack compile errors and three existing call-graph assertions compare an empty `UnreadableSource` slice with nil.

No suppressions or unrelated source repairs were added. Browser verification through agent-browser/Chrome confirmed the option, field and REQUIRES completions and all eight result paths; Lightpanda failed loading the lazy editor component. The isolated server and browser sessions were stopped afterward. Evidence is `.tmp/hack-build-todo-check.log`, `.tmp/hack-build-cli.json`, `.tmp/hack-build-*-completion.txt`, and `.tmp/hack-build-query-ui.txt`.

The small IO fixture contains three Go witnesses sharing one source hash. It performs one cold source-blob read and zero additional warm source reads, with no document-content reads. One measured run took 1.55 ms cold / 0.25 ms warm on SQLite and 4.81 ms / 0.89 ms on PostgreSQL. These are tiny-fixture observations, not real-corpus latency guarantees; `.tmp/hack-build-io.log` records the results.
