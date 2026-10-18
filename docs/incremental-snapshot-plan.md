# Incremental Git snapshots with checkpoints

Status: proposed implementation plan, 2026-10-11, revised 2026-10-18 with the Captain hash-chain review and the documents review. The changes and performance targets below have not been implemented or verified.

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

UIR already stores `BaseSnapshotID`, source set/delete deltas and symbol deltas. `storage.EffectiveSources` and `storage.EffectiveSymbols` resolve the newest binding along recursive base chains. `storage.ActiveDocuments` resolves documents from document set/delete deltas along the same base chain (landed in Phase 2, below). Checkpointing should extend these seams, rather than introduce a second index.

OIPA builds the complete filesystem VFS and catalog in `plugins/oipa/uirindex/directory.go`, archives the repository in `directory_checkout.go`, and publishes the catalog transactionally in `directory_publish.go`. Its `internal/symbols/publish.go` currently prunes with `Keep: 1`, causing survivors whose bases disappear to be rebased into complete source and symbol mappings.

## Hash-chain review (Captain profile, 2026-10-18)

The [Captain profile](../.tmp/captain-profile/findings.md) replays three historical requests in one process against SQLite: commit `567b8ab`, commit `246a1cb` (12 non-test Go files changed in 7 packages), then `246a1cb` again. Hash chains have to serve two uses: answering "has anything in public function A's call graph changed?", and turning a commit into a delta proportional to the change that symboldiff can hydrate cheaply. Today they serve neither, because they chain at package and import-closure granularity.

| Request | Index | Parsed/reused | Captain symbol deltas | Documents phase | Allocated |
| --- | ---: | ---: | ---: | ---: | ---: |
| First commit | 190.2 s | 847/0 | 18,503 | 93.4 s | 19.2 GB |
| 12-file change | 79.8 s | 490/357 | 18,507 | 51.0 s | 6.8 GB |
| Exact repeat | 20.4 s | 0/0, unchanged | 0 | none | 5.5 GB |

Measured read-only on the profile's SQLite database (Captain root only):

- **The amplification is pure waste.** The 12-file change republished 490 Captain documents. 478 of them (62.7 MB) have the same `source_revision_id` and byte-identical `content` as the previous document for their path; only the 12 whose source changed differ (2.6 MB). The 32 changed export shapes altered no downstream document.
- **Most of each document is derivable or duplicated.** The 1,334 Captain documents hold 168.9 MB: 519,280 occurrences and 29,190 declarations. Type-resolution facts take about 81 MB as JSON, almost all repeated 64-hex ids (`occ.enclosing` 36.9 MB, `occ.symbol` 22.6 MB); encoded with symbol handles and local indexes they are about 4.5 MB. Facts derivable from syntax and source bytes take about 73 MB. Full hashes duplicated in `symbol_deltas` take 6.7 MB.

| Finding | Evidence | Consequence |
| --- | --- | --- |
| Export shape is transitive | `exportShapes.workspace` digests every import's shape (`indexer/export_shape.go:80-108`) and feeds `packageInputHash` (`indexer/hashes.go:102`) | One deep public change re-keys every transitive importer: 33 packages and 490 files, 218 of them in 26 packages nobody edited |
| Transitivity is forced by closure facts in per-file documents | `documentBuilder.implements` tests every interface in the transitive import closure (`indexer/typed_document.go:234-300`) | A document is a function of the closure, so its key must be; `implements` also costs 8.7 % CPU on the change and 16.3 % on the repeat |
| Export shape reuses `public_hash` | `functionShape` includes receiver and parameter names; the qualifier is `pkg.Name()` (`indexer/symbol_shape.go:184-235`) | A parameter rename propagates downstream; retargeting `foo/v1.Client` to `bar/v1.Client` does not change the shape |
| Reuse is decided after the work | `reusableRevision` refuses roots with dependencies (`indexer/revision.go:86-91`); every document renders before publication compares `input_hash` (`indexer/modules_documents.go:142-156`) | The repeat typechecks 8.6 s and extracts 10.3 s to publish nothing |
| Delta base is the location head | `loadModuleBase` (`indexer/modules_locations.go:39-71`); historical publication preserves heads (`indexer/publication.go:134-137`) | Captain's primary location has no head, so each historical snapshot is a complete set: `files_added=847`, 18,507 of 18,510 symbols; commits are never diffed against an indexed ancestor |
| No call-graph hash | `content_hash` digests a declaration's own tokens (`syntax.DeclarationContentHash` in `indexer/syntax/declaration.go`); the graph walks one snapshot (`graph/build.go`, `query/graph_source.go:135-173`) | A callee's body change is invisible from A's hashes; answering needs two walks and a manual diff |
| Call-graph edges are incomplete | `implements` sees only the implementer's import closure and skips generic and type-set interfaces; method values and calls through func-typed fields or variables produce no call edge (`indexer/typed_occurrences.go:166-176`); var initializers need `--access read` | A reachability answer built on these edges has false negatives |
| `content_hash` is noisy | Local, parameter and receiver names, gofmt trailing commas and grouping `var` tokens are hashed (`specExtent`, `indexer/typed_document.go:334`) | Renames and re-wrapping report body changes |

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
3. On a new commit, select a compatible indexed baseline, preferring an indexed ancestor. Compare its tree directly with the requested tree; historical order and branches need not follow the current head. Concretely, walk `git rev-list` from the requested commit against stored `snapshots.git_commit` for the same root, configuration and dependency set, and fall back to the location head only when no ancestor is indexed. Historical snapshots then become storage bases for later historical snapshots while still preserving heads. Today `loadModuleBase` (`indexer/modules_locations.go:39-71`) only considers location heads, which is why every Captain historical snapshot was a complete set.
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

The Captain review sharpens these rules:

- **Separate the export shape from `public_hash`.** `public_hash` keeps parameter names and package-name qualifiers because symboldiff reports them as signature changes (`docs/symbol-index-storage.md:280`). The export shape that keys importers should instead digest a name-free API encoding qualified by import path. `encodeType` (`indexer/symbol_shape.go:23`) already produces one and is currently used only for validity checks. A parameter or receiver rename then stops propagating, and a same-name retarget (`foo/v1` to `bar/v1`) starts propagating. Landed as `uir-export-shape-v3` (`symbolResolver.apiShape`); its fold over every import's shape was removed by `uir-export-shape-v4`, below.
- **Make the export shape non-transitive.** Digest the package's own exported API plus the shapes of only those packages that its exported API types reference, which embedding and method promotion need. Do not fold in the whole import closure. Bump `exportShapeVersion` and `packageInputHashVersion`. Landed as `uir-export-shape-v4` / `uir-package-input-v5` (`indexer.apiReferences`, `promotedSelectors`): the shape also digests each exported type's promoted methods and fields, so a method promoted through an unexported embedded field re-keys importers. The Captain replay (`.tmp/captain-profile/wave2a-r2`) re-keys 18 packages and 384 documents for the 12-file change, down from 33 and 490.
- **Move `implements` and `embeds` out of per-file facts.** They are closure facts. Compute them in their own pass as snapshot-level edge deltas, invalidated when a method set changes anywhere in the closure. This is what makes a non-transitive shape sound; leaving them in documents keeps the document a function of the closure. Landed for `implements` as document format 6, `go-types-v5`, and [snapshot implements facts](symbol-index-storage.md#implements-facts) (`implements_deltas`, `implements_facts`): one pass over the whole load per snapshot, recomputed in full and diffed against the base (47 ms for Captain, against 8.7–16 % of CPU before, through a method-name index), which also relates types to interfaces declared outside their import closure. `embeds` stayed in documents: it names the embedded types by identity, a fact of the declaration and its direct imports, not of the closure.
- **Normalize `content_hash`.** Rename locals, parameters and receivers to positional names and drop formatting-only tokens (gofmt trailing commas, the `var` keyword that grouping adds or removes). Bump `contentHashVersion`. Landed as `uir-content-hash-v2`: a trailing comma before `]` is kept, because it is what makes `type A[P *C,]` a type parameter list.
- **Validate against the measured waste.** A replay of the Captain 12-file change must write exactly the 12 changed documents, where today it writes 490 and 478 of them are byte-identical. Report how many packages the narrowed shape still re-extracts, compared with today's 33. Met by document membership (Phase 2): the `.tmp/captain-profile/wave3` replay writes 12 documents while still re-extracting 18 packages.

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

The Captain repeat is the reference case: it currently typechecks for 8.6 s, extracts for 10.3 s and allocates 5.5 GB before reporting `unchanged=true`. The Captain harness (`hack/snapshotbench/captain_run.go`) must also establish a storage base, either with an initial head index or through the ancestor baseline in commit processing. Without one, every historical case publishes a complete set and the replay measures full publication rather than deltas.

### Phase 2: checkpoint and document membership foundations

Generalize `storage/prune_apply.go` to compact an existing snapshot independently of deletion, preserving its semantic metadata and statistics. Add physical-generation selection to source/symbol/document binding keys and effective-chain queries, with one active generation per snapshot and durable candidate metadata. Existing snapshots start in generation zero. Extend effective-state readers and pruning tests to prove checkpoint/delta equivalence on branches and long chains.

Package-wide `input_hash` currently selects every document in a package. Fine-grained reuse needs explicit snapshot document membership. Deliver this first: bind each path to a document by its rendered content digest, and when that equals the base document's digest, reuse the existing document ID and its postings. In the Captain 12-file change that skips 478 of 490 document inserts and their postings; `INSERT symbol_postings` and `INSERT documents` together took 33.9 s of that run. Add document set/delete deltas beside the existing source/symbol deltas, keyed by snapshot/root/path and referencing immutable document IDs, with composite constraints tying a document to its root/path. Resolve effective document bindings along the same base chain and validate their source revision and coverage. Keep package coverage as the package-analysis record rather than assuming every file's document hash equals its package input hash.

Landed as `document_deltas` and `documents.content_digest` ([18_document_membership.hcl](../storage/migrations/18_document_membership.hcl), [document membership](symbol-index-storage.md#membership-which-documents-a-snapshot-activates)). Documents are keyed `(root_id, path_key, content_digest)`, where `storage.DocumentDigest` (`uir-document-digest-v1`) hashes the source revision, package path, indexer version, coverage, and canonical content; `documents.input_hash` stays as provenance, since the SQLite migration cannot drop a column and the cutover needs it to reproduce the old membership. Deltas reference documents by ordinal, and foreign keys tie each delta to its snapshot's root and each document to the delta's root and path. Publication digests every rendered document, binds an existing document under the same key without inserting it or its postings, and writes a delta only where a path's binding differs from the base's. Symbol deltas compare every package with a changed binding whole, because `func init` folds across files. `storage.UirDB` backfills existing databases in bounded, resumable transactions (digests in batches of 64, then one snapshot's membership per transaction, as full sets for roots and differences for descendants); a byte-identical republished document keeps a null digest, so every id, ordinal, and posting survives, and `OpenReadOnly` refuses an incomplete cutover. Captain replay (`.tmp/captain-profile/wave3`) for the 12-file change: 12 documents and 1,959 postings published, against 384 and 40,209 in wave2a-r2; 12 document deltas; documents phase 1.9 s, against 11.3 s; index 9.7 s, against 20.4 s. The 18 re-keyed packages are still type-checked and rendered (Phase 3). Generation-based checkpoint promotion is not part of this change.

Follow models, portable Atlas migration, query/filter wiring, command/API wiring, then focused integration tests. Backfill existing snapshot document bindings using the old exact membership rules in bounded batches: full bindings for root snapshots, and only differences from each base for descendants. Make migration completion explicit; refuse to serve an incomplete cutover. Preserve document IDs, ordinals, sources, payloads, postings, snapshots and foreign keys. Switch all same-module callers to the new membership contract and remove the old runtime lookup path once the migration is complete.

Use metadata-only, paired-key lookups and coordinate the covering-index work with the existing metadata TODO. Avoid the independent `path IN` by `input_hash IN` Cartesian probe and historical payload candidate amplification.

Failing tests first: compact/reopen equivalence, same bytes under different contexts, source/document mismatch, old database preservation, branch isolation, dangling base and invalid operation rejection, and SQLite/PostgreSQL schema integrity. Exit when reads and writes use one validated membership mechanism and checkpoint promotion preserves every semantic result.

### Phase 3: incremental Go extraction

Move input comparison ahead of `extractModule` and the `packages.Load("./...")` call in `typed_load.go`. Until that lands, at least compare each package's `input_hash` with the base's `package_coverage` before `renderDocument` and `implements` run, and reuse the base's document bindings for equal packages (the open item at `docs/benchmarks.md:379`). Reuse unchanged package artifacts and dependency selections; load/typecheck affected package patterns together, preserving one consistent type universe per analysis batch.

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

## Call-graph change queries

"Has anything in public function A's call graph changed between X and Y?" needs no new hash. Answer it at query time:

1. Take the changed set: symbols whose fingerprints differ between X and Y, from `symbol_deltas` through `storage.EffectiveSymbols`.
2. Walk forward from A in Y over call, interface-dispatch, method-value reference and var-initializer edges, and report every path that reaches the changed set.
3. Treat any edge into a module-cache dependency whose selected version changed as changed, because those dependencies have no per-symbol facts.

Expose it as `uir module graph --since <rev>` through `cmd/uir/module_graph_command.go` and `query.Pipeline.Graph` (`query/graph.go:121`). First close the edge gaps listed in the hash-chain review: implementers outside the interface's import closure, generic and type-set dispatch, method values and func-typed fields. A stored, SCC-condensed `closure_fp` per symbol is deferred until the query misses a measured latency budget; a closure hash over incomplete edges would only produce false negatives faster.

## Adversarial review: are documents necessary?

The claim tested: definitions, references and relationships are indexed as rows, and the UI reads a symbol's source from verified source (`query/module_content.go:29-97`), never from document content. So why store documents?

Postings alone are not enough. A `symbol_postings` row is `(document, symbol, role, count)` with no offsets, no enclosing declaration and no fine role (`storage/symbol_models.go:80-88`). Every query reader decodes documents to get them: `query/module_index_ops.go`, `query/module_browse.go:78-140`, `query/sql_native.go`, `symboldiff/documents.go:71-123` and `databasecatalog/diff.go:108-140`. What documents hold falls into four classes, measured on the 168.9 MB of Captain documents:

| Class | Fields | Captain JSON size | Needs storage? |
| --- | --- | ---: | --- |
| Type-resolution facts | occurrence byte span, symbol, role, enclosing; declaration id, extent and name byte spans; `implements`, `embeds` | about 81 MB; about 4.5 MB with handles and local indexes | Yes: they need type information |
| Syntax-derivable facts | line/column ranges, `shape`, `public_shape`, `type_form`, `key`, `parent_key`, `child_slot`, `ordinal`, `payload`, `field`, `identifier`, `semantic_hash`, `enclosing_key`, `target`, `text`, `statement_path`, `resolvable`, `note`, `local_root`; every syntax-coverage document | about 73 MB | No: the pure AST pass (`syntax.Extract` in `indexer/syntax/extract.go`) and source bytes reproduce them, except `shape` and `public_shape` where go/types decides the rendering (see the prototype results) |
| Duplicated fingerprints | full `public_hash`, `private_hash`, `content_hash` | 6.7 MB | No: `symbol_deltas` already stores them |
| Non-Go producer facts | relationships, props, `field` and payload for synthesized sources | not in Captain | Yes: their source exists only in `source_blobs` and has no syntax to re-derive from |

Recommendation: replace Go documents with a compact per-file resolution record holding only the first class, keyed by the digest of the record itself rather than by package `input_hash`. A file whose resolution is unchanged keeps its record when its package is re-typechecked, and postings keep pointing at records. Readers derive the second class at read time from verified source through `readVerifiedSource` and a per-file parse cache keyed by content hash, the same path that call-site guards (`query/graph_guards.go:268-307`) and symboldiff line ownership already take. Non-Go producers keep relationship rows and a payload blob.

Risks:

- `BrowseModules` decodes every active document today; with records it must become lazy per package.
- Historical browsing depends on source staying retrievable: a Git object, or `source_blobs` for non-Git and dirty snapshots. Retention must pin it.
- The occurrence-row design rejected in `docs/symbol-index-storage.md:302` was costed as rows, not as per-file records.

Decision gate, on SQLite: adopt records only if per-package browse and per-symbol query latency (record decode, verified source read and a cached syntax re-derivation) does not regress against today's JSON decode, and the re-derived fields equal the stored ones. Otherwise keep documents but drop the derivable and duplicated classes from them. The prototype in `hack/dochydration` measures this; its results are below.

### Prototype results (SQLite, Captain snapshot at `246a1cb`)

`go run ./hack/dochydration --db .tmp/captain-profile/captain-profiled/uir.db --root github.com/flanksource/captain --out .tmp/captain-profile/doc-hydration` works on a copy of the database and writes `report.md` and `results.json`. The snapshot's 847 active documents hold 108.7 MB.

| Path | All files | `pkg/cli` (173 files) | `pkg/session` (21 files) |
| --- | ---: | ---: | ---: |
| Today: load and decode documents | 2.3–3.7 s, 975 MB allocated | 380 ms | 134 ms |
| Records: size | 3.28 MB (3.0 %) | 0.77 MB | 0.09 MB |
| Records: decode | 20 ms | 4 ms | 0.5 ms |
| Records + re-derivation, warm parse cache | 128 ms | 30 ms | 3.4 ms |
| Records + re-derivation, cold | 7.2 s | 1.5 s | 179 ms |

Each record is checked to decode exactly to the stored type-resolution facts. The record format is varints: declarations carry a symbol handle, extent and name spans, and `implements`/`embeds` handle lists. Occurrences carry a span, the role, a handle, and the enclosing declaration as a local index or a handle. `note`, the type checker's reason a symbol is missing, cannot be recovered from syntax, so it rides in the role byte; it belongs to the stored class.

The cold path is dominated by API overhead, not by reading or parsing. `Pipeline.ReadModuleSource` resolves module scopes and `EffectiveSources` again for every file, about 7 ms per file and 6.1 s of the 7.2 s. Parsing takes 969 ms in total and hydration 152 ms. A batch verified-source reader that resolves the scope once is a prerequisite.

Re-derived fields match the stored ones exactly for:

- every occurrence: range, `note` and `enclosing_key`
- calls: `text`, `resolvable`, `local_root` and `statement_path`; `target` matches 37,501 of 37,502
- declarations: `name`, `extent`, `key`, `identifier`, `parent_key`, `child_slot`, `ordinal`, `payload`, `semantic_hash`, `field`, `kind`, `visibility` and `props`

The exceptions are type renderings:

- **Declarations the syntax pass projects:** `shape` and `public_shape` match 99.7 % and `type_form` 99.96 %.
- **Typed-only declarations:** `shape` and `public_shape` match 75 %.

Every miss needs go/types:

- inferred const and var types, including `iota` and implicitly repeated specs
- anonymous struct and interface literals rendered inline
- constant array lengths (`[sha256.Size]byte` versus `[32]byte`)
- defined types over named types
- generic interface receivers
- one call through an anonymous interface field

Gate outcome: the latency gate passes for warm per-package and whole-snapshot reads, and storage drops to 3 % of today's documents. Shapes are not fully derivable, so `shape` and `public_shape` move to the stored class. Store them only for declarations whose rendering the AST cannot decide, or for all declarations as a deduplicated dictionary in the record. Adopt records on that basis, after the batch source reader lands and cold per-package latency is re-measured against the 134–380 ms decode it replaces.

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
| Export-shape propagation | A parameter or receiver rename does not change importers' input hashes; a same-name import retarget does; an interface implemented outside its import closure is still found. |
| Hash normalization | Local rename, call re-wrap and group move leave `content_hash` unchanged. |
| Ancestor baseline | A historical commit bases on its nearest indexed ancestor, and the 12-file Captain change publishes 12 documents. |
| Call-graph change query | For A → B → C where only C's body changes, `--since` reports A, including through interface dispatch and method values. |
| Resolution records | Records plus re-derived syntax facts reproduce every stored document field. |

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
