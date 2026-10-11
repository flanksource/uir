# Incremental Git snapshots with checkpoints

Status: proposed implementation plan, 2026-10-11. Planning is complete; the changes and performance targets below have not been implemented or verified.

## Outcome

Make committed-source indexing proportional to the affected files, packages and catalog roots. Use persisted extraction results, short delta chains and periodic checkpoints to target less than one second per ordinary commit, while preserving historical queries, source round-trips, coverage, diagnostics, dependency selection and atomic publication.

The first performance gate is p95 below 1,000 ms separately for warm Go and rules sessions. An ordinary commit changes at most five indexed source files and affects at most three packages or catalog roots after dependency propagation, with unchanged producer, toolchain, build policy and global configuration. Classify eligibility from inputs and the dependency graph before examining timing. Also gate unchanged heads and repeated retained historical requests below 1,000 ms. Report every maximum and overrun; a passing p95 does not mean every commit meets the target.

Initial indexing, restart, new toolchain/producer versions, global configuration changes and changes with broad dependency propagation remain measured cohorts. They must remain correct and must not be silently reclassified or omitted to achieve the ordinary-commit target. This plan does not establish a universal sub-second guarantee.

## Evidence and existing seams

The accepted [profiling findings](../.tmp/snapshot-benchmark/findings.md) cover 92 observations over two pinned corpora and head/historical indexing paths. The saved [matrix](../.tmp/snapshot-benchmark/full-20261010-v3/manifest.json) and [report](../.tmp/snapshot-benchmark/full-20261010-v3/report.md) are the diagnostic baseline. Those observations include profiling overhead and concurrent machine activity; establish a separate unprofiled baseline before making performance comparisons.

| Observation | Recorded cost | Implication |
| --- | --- | --- |
| Go head transitions | Median 34.248 s | Whole-module extraction remains expensive despite document reuse. |
| Go retained historical repeat | 18.874 s, including 13.405 s extraction and 3.229 s typechecking | Exact reuse with dependencies is decided after extraction. |
| Go transition `75bbe9533f68` | 17 source revisions, 603 new documents; 14.262 s extraction and 6.404 s document publication | Package input changes amplify document invalidation. |
| Rules head transitions | Median 38.659 s | Full catalog preparation dominates small transitions. |
| Rules transition `14f76826cbbd` | One generated document parsed, 8,593 reused; 20.194 s lowering, 5.372 s archive, 11.359 s publication | Reuse must happen before lowering and publication traversal. |
| Rules repeat | 5.326 s; archive preparation 3.325 s, no lowering/publication region | Move the manifest lookup before archive preparation. |
| Rules pruning in that transition | 2.804 s, nested inside publication | Physical reclamation and full rebasing cannot run on every commit. |
| Rules historical replay | 1,760 snapshots, only 96 additional source revisions | Preserve commit metadata while sharing unchanged content. |

Nested phase durations overlap and must not be added twice. Existing parsed/reused counters describe publication; they do not establish whether extraction or typechecking was avoided.

UIR already stores `BaseSnapshotID`, source set/delete deltas and symbol deltas. `storage.EffectiveSources` and `storage.EffectiveSymbols` resolve the newest binding along recursive base chains. `storage.ActiveDocuments` currently resolves documents through complete per-snapshot package coverage and package input hashes. Checkpointing should extend these seams, rather than introduce a second index.

OIPA builds the complete filesystem VFS and catalog in `plugins/oipa/uirindex/directory.go`, archives the repository in `directory_checkout.go`, and publishes the catalog transactionally in `directory_publish.go`. Its `internal/symbols/publish.go` currently prunes with `Keep: 1`, causing survivors whose bases disappear to be rebased into complete source and symbol mappings.

## Scope and ownership

| Owner | Responsibility |
| --- | --- |
| UIR `indexer/` | Commit identity, affected-package planning, extraction reuse, delta publication and warm session lifecycle. |
| UIR `storage/` | Checkpoint compaction, effective bindings, metadata lookup, retention and concurrency invariants. |
| UIR `query/`, `symboldiff/`, `cmd/uir`, API | Preserve selected-commit behavior and wire any required checkpoint maintenance/reporting options through existing surfaces. |
| OIPA `plugins/oipa/vfs/filesystem`, `plugins/oipa/ast`, `plugins/oipa/uirindex`, `internal/symbols` | Git-backed catalog inputs, XML extraction reuse, dependency propagation and atomic catalog publication. |
| UIR `hack/snapshotbench`, `fixtures/bench/` | Measurement modes, canonical equivalence checks, latency gates and retained evidence. |

Track dependency-aware reuse under existing TODO `8c74f281-a950-4cb9-ad40-11f1b2ade1d8`. Coordinate metadata lookup with `b09d5c4b-bd83-4b63-abe9-2120ebb59936`; do not broaden it into the proposed individual-reference storage redesign. The completed profiling TODO `c2707be9-99cd-4b29-8f9c-af05730dc58f` remains complete.

Unrelated UI/query work, symbol identity redesign, replacing postings, changing extraction coverage, durability settings, local dependency overrides and production deployment are outside this plan.

## Commit processing

1. Resolve the requested Git commit and indexing policy from immutable inputs.
2. Resolve and fingerprint dependency selections. Check for a complete matching published result before checkout, archive, package loading or XML lowering.
3. On a new commit, select a compatible indexed baseline, preferring an indexed ancestor. Compare its tree directly with the requested tree; historical order and branches need not follow the current head.
4. Identify changed inputs, then expand the affected set through recorded semantic dependencies.
5. Reuse valid extraction artifacts for unaffected inputs. Typecheck/lower only the affected set.
6. Publish new source revisions, documents, postings, source/symbol/document deltas and lightweight commit metadata in one transaction. Advance current heads through the existing compare-and-swap; historical requests preserve heads and versions.
7. Record durable maintenance work when checkpoint or reclamation thresholds are crossed. Ordinary commit readiness must not wait for full consolidation.

An exact repeat creates no snapshot or content rows and performs no extraction. A new release/metadata-only commit may create commit snapshot and generated metadata rows, but must not reprocess unchanged source documents.

## Checkpoint representation

A checkpoint is the complete resolved content membership for an existing snapshot. It contains source revision bindings, defined-symbol fingerprints and, once fine-grained document membership is introduced, document bindings. It references existing immutable payloads and postings. It also has access to the package coverage, diagnostics and dependency fingerprints required to decide extraction reuse.

Use the existing root snapshot representation: complete set rows with no storage base. Generalize the current rebasing machinery into a compaction operation rather than adding a separate checkpoint database or copying every JSON payload. `BaseSnapshotID` is a storage dependency; Git parentage remains available from the commit object. Give physical binding sets a generation so a replacement can be prepared without modifying the currently readable state.

Example, with illustrative commit numbers:

```text
C100: checkpoint containing complete source, document and symbol bindings
C101: delta replacing two documents and deleting one source
C102: delta replacing one document
C103: delta with unchanged content and new commit metadata
C103: maintenance consolidates the effective bindings into a checkpoint
C104: delta based on the C103 checkpoint
```

Compaction preserves snapshot ID, ordinal, selected Git commit, producer/configuration identity, effective source/document/symbol membership, coverage, diagnostics, dependency edges and historical visibility. Preserve original publication/change statistics; changing a physical base must not reinterpret an old commit as having added every file. Verify resolved state digests before and after compaction.

Prepare and validate a candidate against a consistent read view, then write its complete binding set into an inactive generation in bounded transactions. Full-set insertion is maintenance work and must not be assumed cheap merely because its inputs were prepared in memory. Throttle batches against foreground publication and record their lock time.

Promote only a complete, verified generation through a short compare-and-swap transaction updating the snapshot's physical generation and base. Recheck snapshot/base generations and the prune epoch before promotion. Reject a stale candidate and retry against current state within a bounded policy. Rollback exposes the previous complete state; readers never consult staged generations. Keep readers on consistent database transactions, protect candidates from reclamation, and clean abandoned generations after restart.

Checkpoint policy belongs in one typed options surface with validated limits. Benchmark candidate maximum chain depths of 16, 32 and 64 and triggers based on accumulated changed-binding count. Select defaults from measured lookup cost, storage amplification and maintenance time. These are experiment values, not established optimal settings. A no-content-change commit should contribute little to the accumulated delta budget, while still counting toward chain depth.

Checkpointing bounds historical reconstruction. It does not by itself avoid XML lowering or Go typechecking; extraction caches and invalidation rules below are required for that benefit.

## Extraction identity and invalidation

Persist artifacts using a versioned identity that includes repository/root, logical path, source digest, producer/indexer format, relevant configuration and effective dependency context. Git object IDs identify Git contents; retain the existing independently verified source digest contract. Paths and embedded locations remain repository-relative and never include scratch-directory names.

| Change | Required work |
| --- | --- |
| Exact retained commit and identical complete context | Return the stored result without extraction. |
| Release/non-indexed file change | New commit metadata as needed; reuse indexed content. |
| Go function body only | Recheck the owning package; reuse unaffected document artifacts only after proving their semantic inputs and diagnostics are unchanged. |
| Go declarations, inferred package values, method sets or exported shape | Recheck the package and propagate every affected semantic dependency. |
| `go.mod`, `go.sum`, local replacement, build tags, GOOS/GOARCH/CGO, tests policy or toolchain | Re-resolve selection/configuration and invalidate affected contexts. Zero changed `.go` files does not imply zero work. |
| XML rule body | Lower the changed rule and its affected consumers. |
| Copybook, schema, hierarchy, override policy, product/plan binding or global catalog change | Recompute the affected dependency closure and generated documents. |
| Rename/delete, package move or root removal | Update source/document/symbol tombstones and catalog heads; validate lookup and old-commit browsing. |
| Producer/indexer identity change | Create artifacts under the new identity and preserve old versions for retained history. |

Exported shape hashes alone are insufficient if extraction consumes private declarations, inferred types, implementations or global catalog facts. Record or conservatively fingerprint those inputs too. An incomplete dependency description causes explicit invalidation of the relevant scope; it must not produce an apparently valid cache hit. Failed dependency resolution and transient infrastructure errors remain errors, never successful cached coverage.

Preserve the existing indexed/partial/syntax/excluded policies. Source-derived diagnostics can be reused only under a proven identical complete context. Do not improve timings by changing a previously typed result to syntax coverage.

## Implementation phases

### Phase 0: establish the acceptance harness

Extend `hack/snapshotbench` with explicit diagnostic-profile and latency modes, a warm session mode, input-based cohort classification and actual work counters. Diagnostic mode continues enforcing the existing profile artifacts. Latency mode records its different artifact contract explicitly and must not relax the diagnostic verifier or reuse stale binaries.

Measure request arrival to durable publication and immediate query readiness. Include commit resolution, dependency checks, extraction, transaction completion and response delivery. Report process startup/database open separately and also publish complete cold CLI timings. Stop the latency timer before the independent full equivalence/source sweep, so verification work is visible but does not masquerade as indexing cost.

Record source bytes read, archive bytes/files, files lowered, packages loaded/typechecked, reused extraction artifacts, documents/postings written, source/symbol/document deltas, chain depth, checkpoint/reclamation time and queued maintenance size. Label SQL totals from instrumented diagnostic runs; expensive SQL recording stays off in latency runs.

Add focused failing Ginkgo tests for the new report contract and verifier before implementing the harness changes. Produce an unprofiled baseline for both corpora using the current producer. Exit when cohort selection is deterministic, incomplete reports fail, and diagnostic versus latency artifacts cannot be confused.

### Phase 1: exact reuse and incremental Git inputs

In `indexer/revision.go` and dependency selection, resolve existing dependency identities without extracting or publishing each inbound edge repeatedly. Memoize selection and extraction within one run using full identity, not only a path. Match a complete stored result before preparing a full workspace. Use the existing TODO for this seam; removing the dependency guard without proving context is incorrect.

In OIPA, read the effective committed configuration before archive preparation, compute producer identity once per session, open the output store and call the manifest lookup first. Preserve the full `ResolveOipaConfig` contract, including applicable configured inputs outside the selected repository; a committed `oipa.yaml` alone is not automatically the complete policy.

Use Git tree comparisons for changes and a reusable object reader for changed blobs. [Git `diff-tree`](https://git-scm.com/docs/git-diff-tree) compares the trees; [Git `cat-file`](https://git-scm.com/docs/git-cat-file) supports batched object reads. Parse raw/NUL-delimited paths without shell interpolation. Handle deletes, moves, submodules, modes and the existing symlink policy explicitly; reject unsupported inputs loudly.

Rules inputs should use the existing VFS/provider boundary to read Git objects without a whole archive. Native Go uses a stable isolated materialized workspace or supported overlay, updating changed inputs without modifying the user's checkout. A complete workspace build is the defined cold-input path, not an error-swallowing recovery path.

Failing tests first: exact repeat with dependencies must never invoke the package loader; exact rules repeat must never archive; changed config/dependency identity must invalidate; historical requests must preserve heads. Exit when those tests pass and work counters confirm the avoided work.

### Phase 2: checkpoint and document membership foundations

Generalize `storage/prune_apply.go` to compact an existing snapshot independently of deletion, preserving its semantic metadata and statistics. Add physical-generation selection to source/symbol/document binding keys and effective-chain queries, with one active generation per snapshot and durable candidate metadata. Existing snapshots start in generation zero. Extend effective-state readers and pruning tests to prove checkpoint/delta equivalence on branches and long chains.

Package-wide `input_hash` currently selects every document in a package. Fine-grained reuse needs explicit snapshot document membership. Add document set/delete deltas beside the existing source/symbol deltas, keyed by snapshot/root/path and referencing immutable document IDs, with composite constraints tying a document to its root/path. Resolve effective document bindings along the same base chain and validate their source revision and coverage. Keep package coverage as the package-analysis record rather than assuming every file's document hash equals its package input hash.

Follow models, portable Atlas migration, query/filter wiring, command/API wiring, then focused integration tests. Backfill existing snapshot document bindings using the old exact membership rules in bounded batches: full bindings for root snapshots, and only differences from each base for descendants. Make migration completion explicit; refuse to serve an incomplete cutover. Preserve document IDs, ordinals, sources, payloads, postings, snapshots and foreign keys. Switch all same-module callers to the new membership contract and remove the old runtime lookup path once the migration is complete.

Use metadata-only, paired-key lookups and coordinate the covering-index work with the existing metadata TODO. Avoid the independent `path IN` by `input_hash IN` Cartesian probe and historical payload candidate amplification.

Failing tests first: compact/reopen equivalence, same bytes under different contexts, source/document mismatch, old database preservation, branch isolation, dangling base and invalid operation rejection, and SQLite/PostgreSQL schema integrity. Exit when reads and writes use one validated membership mechanism and checkpoint promotion preserves every semantic result.

### Phase 3: incremental Go extraction

Move input comparison ahead of `extractModule` and the `packages.Load("./...")` call in `typed_load.go`. Reuse unchanged package artifacts and dependency selections; load/typecheck affected package patterns together, preserving one consistent type universe per analysis batch.

Keep cache contents as versioned extraction data and dependency fingerprints. Do not merge raw `types.Object` or `types.Type` instances from independent loads: [the packages documentation](https://pkg.go.dev/golang.org/x/tools/go/packages) explicitly identifies incompatible type identity across calls. If affected-package loading still dominates the one-second budget, profile that phase and extend metadata/export-data reuse through the same owner seam before claiming the target is met.

Initially preserve conservative package invalidation. Then split each document's own source input from the semantic context it actually consumes, with explicit dependency/diagnostic fingerprints. Reuse an unchanged file's document only when that identity is proven stable. Changing an inferred package variable, private declaration, method set or relevant import must invalidate dependent facts even if public signatures look unchanged.

Keep structured `DocumentContent` through symbol-fact construction and validation; serialize once at the storage boundary. Eliminate the current marshal/unmarshal cycle in `withSymbolFacts` without weakening symbol, owner, occurrence or kind validation.

Failing tests first: body-only changes, private/inferred declaration changes, exported-shape propagation, interface implementations, dependency-only changes, build variants, test policy, partial diagnostics and loader invocation counts. Exit when canonical full-versus-incremental documents match and unaffected packages are neither typechecked nor lowered.

### Phase 4: incremental rules extraction

Extend `plugins/oipa/vfs/filesystem` and AST/catalog lowering with versioned per-input artifacts and dependency records. Cache XML decoding, rule/copybook lowering and stable catalog entities. Update the affected entities in the VFS and rebuild only the affected publications in `internal/symbols`.

Track copybook/schema use, hierarchy and override resolution, product/plan bindings, generated metadata and any global enumeration that affects lowering. Model a global dependency explicitly when the result consumes the complete catalog. Discovery of a changed global input may legitimately require broad work; a cache hit may not conceal it.

Reuse the existing source-root registry and path identity rules. Git-backed input positions and payloads must remain equal to complete filesystem extraction. Preserve reported filesystem coverage and database-only omissions.

Failing tests first: one rule edit, unchanged release, copybook propagation, schema change, hierarchy/override changes, rename/delete/root removal, cross-root references and exact source positions. Exit when complete catalogs match the independent full producer and work counters demonstrate affected-only lowering.

### Phase 5: delta publication and warm sessions

In `modules_sources.go`, `modules_documents.go`, `publication.go` and the OIPA publisher, consume planned changes and reused references directly. Batch metadata lookups and writes. Validate newly generated structured documents once at the ingestion boundary, reuse already validated artifacts only under their complete identity, and preserve conflicting-publication invariant checks.

Write postings for new documents only. Produce source, symbol and document deltas from the affected set. Derive snapshot totals from validated prior totals plus changes, with independent full recomputation in tests and diagnostic verification. Do not scan or decode all document payloads merely to publish one changed file.

Retain lightweight per-commit root snapshots and the complete atomic catalog manifest so existing snapshot IDs, commit selectors and `snapshot.GitCommit` behavior remain meaningful. Batch the unchanged roots' commit metadata and coverage associations while sharing their content. Avoid introducing a new commit-to-content lookup API unless evidence shows those small metadata rows dominate; changing commit identity checks merely to permit stale module metadata is not acceptable.

Add a bounded warm session behind the existing indexer/producer APIs, with database connections, Git readers and extraction caches. Reuse established Clicky/Gavel process and task supervision rather than adding a daemon framework. Close resources, cancel requests and cap cache memory explicitly. Persisted artifacts allow correct restart; in-memory caches are accelerators, not the sole copy of required state.

Failing tests first: same commit idempotency, commit metadata without new content, duplicate writer conflicts, atomic catalog rollback, cancellation and current/historical query readiness. Exit when ordinary commits publish changed content plus small metadata and historical requests never move heads.

### Phase 6: bounded checkpoint and reclamation maintenance

Remove per-root physical pruning from the OIPA publication transaction. Preserve the visible current/historical retention contract while keeping internal bases reachable until consolidation completes. Record durable maintenance demand atomically with publication; restart must discover unfinished work.

Maintain bounded internal chains and a measured backlog. Schedule checkpoint preparation before the hard limit, keep promotion transactions short, and apply explicit backpressure if maintenance cannot keep up. Publish maintenance lag and any foreground delay; do not hide those commits from latency reports. A warm sequential replay should remain below target while maintenance runs.

After checkpoint promotion, reclaim only snapshots/content unreachable from visible heads, retained historical commits, catalog manifests, dependency targets and active maintenance/read leases. Catalog module references currently live in manifest content, so protect them explicitly in the retention plan; foreign keys alone do not establish their liveness. Preserve the history-prune epoch/cache invalidation contract.

Keep explicit operator pruning synchronous and truthful about its requested retention result. Internal checkpoint bases may be physically retained while hidden from the visible retention result. Test this distinction and document it. Garbage collection must never silently delete a retained historical catalog or a dependency target.

Failing tests first: crash before/after promotion, concurrent reader/publisher, stale candidate, branch bases, multiple heads, catalog references, dependency targets, pinned historical snapshots, backlog limits and restart recovery. Exit when long replay keeps chain/read costs bounded and every retained commit remains equivalent before and after maintenance.

### Phase 7: final corpus and product validation

Run the original diagnostic replay on both pinned corpora and both indexing paths; preserve its 92-case structure and full source/profile evidence. Add an independent full-extraction oracle in separate databases and compare canonical source hashes, documents, definitions, occurrences, relationships, effective symbol fingerprints, coverage and diagnostics for every commit. Normalize only database-local IDs/ordinals and expected timing/publication metadata, never semantic facts.

Use unprofiled latency passes with fresh output databases and stable binaries. Repeat the complete sequence five times per corpus/path, serially, warming each session with its baseline commit. Combine only identically classified cases, retain all samples, and report per-case distributions when sample counts permit. Small cohorts must state their sample count and include deterministic small-change transitions anchored to the real corpora if the pinned histories provide inadequate coverage. Never combine the two producers or profiled/unprofiled samples into one percentile.

Exercise at least 1,000 delta links, a branch and merge, deletion/rename, exact repeats, config/dependency invalidation and maintenance running concurrently. Force small checkpoint thresholds in correctness tests to cross boundaries frequently; benchmark production candidate thresholds separately. Include interruption/restart and a long sustained replay to prove that foreground speed does not accumulate unbounded maintenance debt.

Expose all gates through a new Gavel acceptance fixture, planned as `fixtures/bench/snapshot-checkpoints.md`. It invokes focused Ginkgo tests, corpus equivalence, latency report validation, API integration, build and lint. Use tables with JSON/CEL assertions for the evidence checks. Implement a strict harness verifier/report contract for checkpoint semantics and separate per-corpus p95/maximum values; missing samples, stale fingerprints, incorrect classification or absent equivalence evidence fail loudly.

The fixture is a planned deliverable, not an implemented verifier in this planning change. Scaffold its real commands with focused failing tests during Phase 0, inspect it with `gavel fixtures outline`, execute it before attaching it, then attach it as the implementation TODO's executable definition of done. Final acceptance runs only through `gavel todos check`; the individual commands below are fixture building blocks, not a parallel verifier.

## Required test coverage

| Area | Observable assertion |
| --- | --- |
| Exact reuse | Loader/lowerer/archive invocation counters remain zero; no structural writes. |
| Content changes | Changed documents and references match full extraction; unaffected artifacts retain identity. |
| Dependency/configuration changes | Correct affected closure, no stale hits, unchanged `.go` bytes do not bypass dependency checks. |
| Coverage | Full and incremental coverage/diagnostics are equal, including older partial/syntax snapshots. |
| Source identity | Every sampled retained source matches selected-commit bytes and checksum; no scratch paths. |
| Checkpoint equivalence | Effective sources, documents, symbols, relationships, diagnostics and original statistics match before/after/reopen. |
| Branches/history | Exact requested commit resolves; historical requests preserve heads and versions across branches and merges. |
| Catalog atomicity | Module snapshots, root removals, heads and manifest commit together or all roll back. |
| Retention/concurrency | Pinned history, manifest references and dependency targets survive; no reader observes partial promotion or missing bases. |
| Migration | Existing SQLite/PostgreSQL histories, payloads, IDs, ordinals, postings and integrity survive bounded backfill. |
| Resource bounds | Chain limits, session cache limits and durable maintenance backlog are enforced and reported. |
| Performance | Independent Go/rules warm p95 values and repeat/no-op gates are below 1,000 ms; all overruns and cold/global cohorts remain reported. |

Pure invalidation/hash/threshold logic belongs in colocated Ginkgo unit tests. Database/checkpoint/migration tests remain separate integration tests on both supported engines. Extend `packages/api/test/*.spec.ts` for changed publication/maintenance/query behavior. Add no unrelated demo tests.

## Gates and runnable evidence

Existing commands below are available now; executing them is implementation verification work. New latency/equivalence/checkpoint commands must be implemented and documented in Phase 0 before the fixture depends on them.

```bash
go test ./indexer ./storage ./query ./symboldiff ./cmd/uir ./hack/snapshotbench
```

```bash
pnpm --dir packages/api test
```

```bash
make build
```

```bash
make lint
```

```bash
go test ./...
```

```bash
go run ./hack/snapshotbench run --out .tmp/snapshot-benchmark/checkpoint-profile
```

```bash
go run ./hack/snapshotbench verify .tmp/snapshot-benchmark/checkpoint-profile
```

Build the OIPA producer with its own `make build`, run focused Ginkgo integration tests in the owning VFS/AST/index/publication packages, and run producer lint. Record unrelated dirty-worktree baseline failures separately; never silence them through `.gavel.yaml` or weaken assertions. Update README/storage/history docs and the ERD for actual landed storage changes. Regenerate root UIR schema only if root model registries change.

After automated gates pass, perform local API source/query smoke checks against current, historical-only and checkpoint-compacted snapshots. If the Explorer's snapshot/maintenance behavior changes, inspect it live using agent-browser. No production writes or external publication are part of verification.

## Delivery order and completion

Implement Phase 0 first. Deliver exact repeat reuse and Git input work before broad extraction changes. Establish checkpoint/document membership semantics before enabling fine-grained Go/rules reuse. Then batch delta publication, integrate bounded maintenance and run the complete validation matrix. Preserve independent phase evidence so a latency improvement cannot hide a correctness regression.

Completion requires a passing canonical Gavel fixture, persisted reports with exact corpus/producer identities, per-corpus latency results, full-versus-incremental equivalence, bounded maintenance under sustained load and documented current/historical/checkpoint behavior. If affected Go typechecking or storage promotion remains over budget, report the measured residual phase and keep the performance criterion open; do not declare completion from no-op timings or extrapolated savings.

The current planning deliverable changes documentation and tracked plan metadata only. No implementation run, staging, commit or push is included.
