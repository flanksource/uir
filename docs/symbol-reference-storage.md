# Native UIR references and symbol locations

**Status: proposed design, revised 2026-10-09.** This document specifies a replacement for the current `symbol_postings` query model, makes the existing 64-bit handle the sole symbol ID and records the compact-position design and 2026-10-08 corpus findings. The accompanying [Facet ERD source](uir-gorm-erd.tsx) and [rendered ERD](uir-gorm-erd.html) show the proposed relationships; their coordinate rows still show the earlier expanded layout and await the [compact-position update](#erd-rendering). The current [Atlas HCL](../storage/migrations/06_symbol_index.hcl) and [storage API](../storage/README.md) remain the implemented schema. The compact codec, projections and migration remain unimplemented.

## Decision and purpose

Store individual references in `symbol_references` and declaration positions in `symbol_locations`. Replace every `symbol_postings` consumer, including graph traversal, exact references, declaration lookup, historical diff support and pruning. Retain document JSON for rich AST payloads, explorer detail, shapes, diagnostics and historical diffs. Publication and historical backfill derive the query projections from the same validated extraction.

Keep each position's start byte offset as an indexable bigint and store the remaining coordinates in a compact binary record. Preserve exact name and extent ranges, UTF-16 columns and byte spans. Location readers reconstruct those facts from the selected projection rows without reading source files or decoding document JSON.

Use `symbols.id BIGINT PRIMARY KEY`, taking each existing row's `handle` value without renumbering. Remove the SHA-256 symbol ID, the duplicate `handle` column and `identity_version`. All owner, reference, location and delta symbol keys use this one ID. There is no portable symbol identity contract, retained hash alias or second public ID. Producers supply symbol metadata and references local to their publication; storage recognizes existing symbols and allocates IDs.

The current inverted index identifies documents that mention a symbol and records aggregate counts. [Incoming references](../query/module_index_ops.go) then decode those documents to recover occurrence kind, enclosing caller and position; outgoing traversal scans the defining document's occurrences. The new tables answer those questions directly. A call graph still reads verified source when deriving call text and guards, and rich payload requests can still load saved document JSON.

The [2026-10-07 review](#corpus-and-performance-validation) also found that content-free membership reads fetched `ordinal` after the large JSON column. A covering metadata index and exact extraction-key lookup address that separate membership cost. Indexed reference rows address subsequent document decoding.

## Identity, versions and positions

### Permanent symbol IDs

Within a database, a symbol keeps its ID across snapshots, checkouts, disappearance and reappearance, pruning, reopening and reindexing. `symbols` becomes a permanent identity registry: retain every committed mapping, including symbols with no surviving declaration or reference. Retain its owner rows and the module/package/kind allocation registries too. IDs are never reassigned to different symbols. Two independently created databases can allocate different IDs for the same metadata; copying a database preserves its mappings.

Keep the current H64b layout, `0 | module 11 | package 16 | visibility 1 | kind 6 | local 29`, and its positive signed-64-bit values. Resolve the complete natural key first; only a missing key allocates the next local value in its bucket. Since committed rows are never deleted, the largest allocated value remains available to prevent reuse. Allocation and insertion occur in the publication transaction; concurrent allocation conflicts retry the whole transaction using the existing conflict handling. Uncommitted allocations are not published identities.

Module, package, kind and visibility allocation metadata must agree with the stored ID, as the current allocator requires. Conflicting metadata is an error, never a request to repack an existing ID. Reindex clears extraction/snapshot facts according to its scope but retains symbol mappings, namespace registries and `symbol_layout`. An incompatible layout requires an explicit upgrade; remove the automatic generation-discard path that drops those mappings. Stability applies to mappings present at cutover; an old mapping already deleted by the current pruning policy cannot be recovered without historical evidence.

### Extraction versions and positions

`version_id` is the existing `documents.ordinal`, identifying one immutable extraction of a file under its package input hash. It is different from the document JSON format version, a snapshot ordinal and the head's compare-and-swap version. A dependency or build-context change can change reference resolution without changing the file bytes. `source_revision_id` identifies those bytes; the extraction key also captures package input and indexer context.

Snapshots select active extraction versions through effective sources and package coverage. Each `(root_id, path_key, input_hash)` selects exactly one document. Several snapshots and checkouts may share a version's locations and references. Deleting or moving a file changes snapshot membership rather than rewriting historical rows.

An edge belongs to its source extraction version. Its target is a symbol ID; the selected snapshots determine which target declaration versions are available. Cross-module edges do not require an arbitrary target-version choice at publication time. A permanent registry row alone does not make a symbol present in a selected snapshot: readers must apply effective-version and symbol-delta membership.

Keep existing coordinate conventions: ranges use one-based lines and one-based UTF-16 columns, with an exclusive end position; byte spans use zero-based, half-open `[start_offset, end_offset)` offsets. The line/column end identifies the same exclusive position as the byte-span end. Copy validated positions without converting columns to bytes. `symbol_locations` positions describe declarations; `symbol_references` positions describe individual uses.

## Proposed schema

PostgreSQL uses bigint/integer, UUID, JSONB, text, boolean and BYTEA. SQLite uses the existing portable HCL projection, including text UUIDs, validated JSON text and BLOB for binary position records. All columns below are required unless marked nullable. Text fields carrying an absent optional fact use the same empty-string representation as the document model; missing identities use SQL NULL.

### `symbols`

One permanent mapping from complete symbol metadata to an allocated ID. This table changes in place through the controlled migration below; it is not a second registry alongside the current hash-keyed table.

| Column | Type | Meaning |
| --- | --- | --- |
| `id` | bigint, PK | Existing H64b handle; positive, immutable, allocated explicitly |
| `canonical_key` | text, unique | Internal natural-key encoding for recognizing an existing symbol |
| `module_key` | text | Declaring module key; existing builtin convention retained |
| `package_path` | text | Declaring package path |
| `kind` | text, FK | Registered `symbol_kinds.name` |
| `owner_id` | bigint, nullable, FK | Owning symbol's `symbols.id`; self-FK with delete restriction |
| `name` | text | Symbol name |
| `search_name` | text | Existing search normalization |
| `visibility` | text | Existing exported/internal allocation metadata |
| `parameter_types` | JSONB | Ordered parameter type strings; empty array when absent |

The unique natural key encodes `(module_key, package_path, kind, owner_id, name, parameter_types)` using length-delimited fields, an explicit absent-owner encoding and an ordered parameter count/list. Encode the resolved owner ID as decimal text. This recognizes the same symbol on subsequent indexing; it is neither a hash nor an exported identity. Freeze this internal encoding for the database schema version and validate it against the row's metadata. Resolve owners before children, reject ownership cycles and preserve overload distinctions. Keep search and metadata lookup indexes, now ending in or referring to bigint `id`. Remove hash-length checks and the separate handle index; the PK supports H64b range scans.

### Retained document and API IDs

Introduce document format 4. Store symbol IDs as a checked 64-bit type internally and canonical decimal strings in JSON, preserving all bits in JavaScript clients. Require positive, in-range decimal strings with no leading zeros; SQL NULL and absent optional JSON IDs retain their existing meanings. Do not send these IDs as JavaScript numbers or accept retired hashes as aliases.

Replace canonical IDs in `DocumentSymbol.ID`, `Implements`, `Embeds`, `Occurrence.Symbol` and `Occurrence.Enclosing`. Preserve `DocumentSymbol.Key`, `Occurrence.EnclosingKey` and full `uir.Identifier` values, including producer UUIDs: these serve explorer navigation and domain metadata. Source/content/input hashes and shape/body fingerprints retain their separate purposes. Upgrade saved JSON with a typed rewrite of symbol-ID fields and the format discriminator; all other saved facts and array order survive.

API symbol IDs, selectors, graph symbol-node IDs, reference results and explorer symbol fields use the same decimal-string encoding. Update OpenAPI, generated clients, UI comparisons, caches and fixtures together. The generic `graph` package retains string node keys because it also represents unresolved and file-scope nodes and non-storage producers; storage adapters format a real symbol's bigint ID into that string. Do not introduce storage dependencies into the language-neutral graph model. `SymbolID(Identity)` hashing and ID-to-handle translation APIs are removed after their callers migrate.

### `symbol_locations`

One row per `DocumentContent.Symbols` entry. `declaration_index` is its zero-based position in that saved array. Use the array position in the primary key to preserve unproven declarations and avoid imposing a new uniqueness assumption on `(symbol_id, version_id)`.

| Column | Type | Meaning |
| --- | --- | --- |
| `version_id` | bigint, PK, FK | `documents.ordinal` |
| `declaration_index` | integer, PK | Zero-based declaration-array index |
| `symbol_id` | bigint, nullable, FK | `symbols.id`; NULL for unproven declarations |
| `root_ordinal` | integer, FK | Source module's `modules.ordinal` |
| `source_revision_id` | UUID, FK | The document's `source_revisions.id` |
| `identity_key` | text | `DocumentSymbol.Key` |
| `identifier` | JSONB | Full structured `uir.Identifier`, including producer IDs |
| `declaration` | text | Existing declaration/shape text |
| `child_slot` | text | Explorer capability metadata; empty for a non-explorable entry |
| `name_start_offset` | bigint | Declaration name byte-span start |
| `position` | BYTEA / BLOB | Compact name range/length and full declaration extent; format below |

The location stores the source revision rather than duplicating the file path, checkout path or snapshot ID. File paths and hashes come from `source_revisions`; the selected scope supplies checkout and snapshot. Rich entry payloads, field detail, shape/body hashes and remaining AST metadata stay in document JSON.

Primary key: `(version_id, declaration_index)`. Secondary indexes:

- `symbol_locations_symbol_idx (symbol_id, root_ordinal, version_id, declaration_index)` for declarations of selected symbols.
- `symbol_locations_source_idx (source_revision_id, name_start_offset, version_id)` for file-position lookup.

The projection builder validates that root and source revision match the referenced document and that `identifier.IdentityKey()` equals `identity_key`. Declaration indices are nonnegative; name and extent ranges satisfy existing document validation, including name containment in the extent.

### `symbol_references`

One row per `DocumentContent.Occurrences` entry, including definition occurrences. These original positions are needed by exact-reference lookup. Append declared `implements` and `embeds` relationships after occurrence rows, visiting declaration entries and their relationship arrays in saved order. `reference_index` is the resulting zero-based sequence number. Never collapse repeated source/target/kind triples or equal offsets.

| Column | Type | Meaning |
| --- | --- | --- |
| `version_id` | bigint, PK, FK | Source extraction's `documents.ordinal` |
| `reference_index` | integer, PK | Deterministic occurrence/relationship sequence index |
| `root_ordinal` | integer, FK | Source module's `modules.ordinal` |
| `from_symbol_id` | bigint, nullable, FK | `symbols.id` of the enclosing symbol, or declaring symbol for implements/embeds |
| `from_key` | text | Original enclosing identity key, or declaring entry's key for implements/embeds |
| `to_symbol_id` | bigint, nullable, FK | Referenced symbol's `symbols.id` |
| `kind` | integer | Reference kind from the code table below |
| `start_offset` | bigint | This occurrence's byte-span start |
| `position` | BYTEA / BLOB | Compact occurrence range and byte length; format below |
| `target` | JSONB, nullable | Original structured call target, including unresolved locators |
| `statement_path` | text | Original call locator's statement path |
| `text` | text | Original occurrence/call text |
| `note` | text | Original explanation for an unproven target |
| `resolvable` | boolean | Original locator flag |
| `local_root` | boolean | Original locator flag |

| Code | Kind | Origin |
| --- | --- | --- |
| 0 | definition | Occurrence role `definition` |
| 1 | reference | Occurrence role `reference`; data-use graph edges map this to read |
| 2 | call | Occurrence role `call` |
| 3 | write | Occurrence role `write` |
| 4 | type | Occurrence role `type` |
| 5 | import | Occurrence role `import` |
| 6 | implements | Declaration's `Implements` array |
| 7 | embeds | Declaration's `Embeds` array |

Define one typed integer enum with checked conversions to/from existing role strings. Unknown kind codes or role names are errors. This enum replaces the old aggregate posting-role representation; its code 2 means call, so it must never be cast from the old implements code.

Primary key: `(version_id, reference_index)`. Secondary indexes:

- `symbol_references_from_idx (from_symbol_id, kind, root_ordinal, version_id, reference_index)` for outgoing traversal.
- `symbol_references_to_idx (to_symbol_id, kind, root_ordinal, version_id, reference_index)` for incoming traversal and exact uses.

For occurrence rows, copy the resolved bigint IDs in `Enclosing` and `Symbol` directly, preserving `EnclosingKey`, locator fields and note. A file-level occurrence has no enclosing symbol. Syntax and partial documents may have an unproven target or enclosing declaration; their existing coverage validation governs which facts can be absent. An unresolved call may retain a structured target with no symbol ID. Do not invent a symbol or erase an unresolved occurrence to satisfy a foreign key.

For implements/embeds rows, both symbol IDs are required and `from_key` identifies the declaring entry. Use that entry's name range and byte span as the relationship's declaration position; it is not an exact interface-clause occurrence. `target` is NULL, optional text fields are empty, and locator flags are false. Dispatch edges remain derived at query time from interface relationships in the selected versions.

The builder checks that every non-null enclosing occurrence ID belongs to a declaration of the same document, as current `DecodeDocument` requires. `from_key` preserves the saved key when present; canonical enclosing lookup otherwise uses the same ID-to-entry selection as the current decoded document. This preserves documents with several entries without adding a symbol/version uniqueness constraint.

### Compact position encoding

The physical layout replaces twelve declaration-coordinate columns with `name_start_offset` and `position`, reducing `symbol_locations` from 21 to 11 columns. It replaces six occurrence-coordinate columns with `start_offset` and `position`, reducing `symbol_references` from 19 to 15 columns. Keys, foreign keys and indexes retain the definitions above. Line and column coordinates are decoded result fields; the database indexes symbol IDs, extraction versions and the retained start offset.

Store raw binary bytes. Use canonical unsigned LEB128 varints and signed ZigZag varints through Go's [encoding/binary](https://pkg.go.dev/encoding/binary). The encoding handles signed-64-bit coordinate arithmetic with checked conversion to the existing `Range` and `ByteSpan` element types; it introduces no fixed line, column or file-size bit allocation.

Each record begins with one header byte:

| Bits | Meaning |
| --- | --- |
| 0–3 | Binary format version, initially 1 |
| 4 (`0x10`) | Primary span crosses lines |
| 5 (`0x20`) | Declaration extent follows |
| 6 (`0x40`) | Single-line UTF-16 width equals byte length; omit the redundant width |
| 7 (`0x80`) | Reserved; must be zero |

Encode the primary span in this order, using unsigned varints:

1. Byte length, `end_offset - start_offset`; the start offset comes from the SQL column.
2. Start line.
3. Start UTF-16 column.
4. For a multiline span, positive end-line delta followed by absolute end UTF-16 column.
5. For a single-line span, positive UTF-16 column length, omitted when bit 6 is set.

Set bit 6 whenever a single-line span's supplied column length equals its byte length. Determine equality from both supplied coordinate facts; an ASCII assumption would lose Unicode positions. For a multiline span bit 6 must be clear. Decode the exclusive byte end by adding byte length to the SQL start offset. Decode a single-line end column by adding the explicit column length, or the byte length when bit 6 is set. Decode a multiline end line by adding the line delta, retaining its explicit end column.

Declaration records require bit 5 and append the following six values. Reference records require bit 5 to be clear, including implements/embeds rows that use the declaration's name as their primary span:

1. Unsigned byte prefix gap, `name_start_offset - extent_start_offset`.
2. Unsigned byte suffix gap, `extent_end_offset - name_end_offset`.
3. Signed start-line delta, `extent_start_line - name_start_line`.
4. Signed start-column delta, `extent_start_column - name_start_column`.
5. Signed end-line delta, `extent_end_line - name_end_line`.
6. Signed end-column delta, `extent_end_column - name_end_column`.

The signed deltas preserve columns even when the extent endpoints lie on different lines from the name. Zero byte gaps and zero coordinate deltas are valid. Name and extent byte spans remain independently reconstructable and the name must remain inside the extent.

For example, a three-byte name at line 111, column 6 has this reference record; all hex pairs below are individual stored bytes:

```text
start_offset = 1000
position     = 41 03 6f 06
decoded      = bytes [1000,1003), range [111,6,111,9]
```

Use one codec in the projection builder, backfill and storage readers. Reject unsupported versions, reserved or inconsistent flags, the wrong extent flag for the row type, truncated/overlong/noncanonical varints, overflow, trailing bytes, invalid ranges and invalid extent containment. Require positive lines/columns and lengths, nonnegative start offsets, and an exclusive end after the start. Use checked arithmetic for reconstruction and surface errors with the version ID and declaration/reference index. Retain expanded coordinates in document JSON and API responses.

Binary format 1 is included in the proposed `document_projection_status.projection_version = 1`; no deployed projection format is being upgraded. The binary format discriminator is separate from the format-4 document-ID migration. Historical backfill encodes the validated coordinates already saved in document JSON, so the position change requires no source fetch or reindex.

### `document_projection_status`

This table distinguishes a complete empty projection from a document never migrated.

| Column | Type | Meaning |
| --- | --- | --- |
| `version_id` | bigint, PK, FK | `documents.ordinal` |
| `projection_version` | integer | Query projection format, initially 1 |
| `declaration_count` | integer | Number of location rows, equal to document `symbol_count` |
| `occurrence_count` | integer | Number of reference rows of kinds 0–5, equal to document `occurrence_count` |
| `relationship_count` | integer | Number of reference rows of kinds 6–7 |

All counts are nonnegative. Write the status after validating the rows, in their transaction. Excluded and empty documents receive a zero-count status. Readers reject missing, unsupported or inconsistent status rather than silently returning empty data. Cache verified immutable completeness with snapshot facts; validation must not introduce per-occurrence queries.

### Keys, deletion and membership metadata

Each new table's `version_id` FK cascades when its document is deleted. Location/reference symbol-ID FKs restrict symbol deletion; ordinary pruning never deletes registry rows. Root FKs reference `modules.ordinal`; projection construction and migration validation check that each root equals the document's owning root. Location source FKs restrict deletion of revisions still used by a projection. Delete version facts and source data in the existing child-first order.

Retain document primary keys, extraction-key uniqueness and ordinals; the controlled ID rewrite upgrades JSON to format 4. Add a covering metadata index:

```sql
CREATE INDEX documents_membership_metadata_idx ON documents (
  root_id, path_key, input_hash, id, ordinal, source_revision_id,
  package_path, indexer_version, coverage, symbol_count, occurrence_count
);
```

Use an explicit metadata projection and batches of 256 exact `(path_key, input_hash)` pairs under one root, expressed as a `VALUES` relation joined to `documents`. Preserve missing-document and revision-mismatch errors. Independent path/hash `IN` lists would probe combinations the snapshot does not activate. Inspect SQLite's actual generated migration SQL and query plan; reordered struct/HCL fields alone do not repair a populated database.

## Publication and query flow

### Producer references and allocation

Replace the external producer's hashed-ID contract with publication-local references. `PublishedSymbol.Key` is required and unique within the publication; `OwnerKey` optionally names another supplied symbol. Metadata carries module, package, kind, name, ordered parameters and visibility. Every declared/referenced symbol and every owner must be supplied, as in the current completeness contract. Keys need not be stable across publications.

Use a producer input type separate from stored `DocumentContent`. Its declarations carry nullable `SymbolKey` and implements/embeds key arrays; occurrences carry nullable `SymbolKey` and `EnclosingSymbolKey`. Keep the existing declaration `Key` and occurrence `EnclosingKey` as explorer locators, distinct from publication-local symbol keys. Validate duplicate/missing keys, kind registration, metadata conflicts and owner cycles before committing any facts.

Inside the publication transaction, resolve owners, find or allocate each natural key and build a publication-key-to-bigint map. Convert only the typed symbol-reference fields to stored IDs, then validate and persist the format-4 document. Input keys do not become database aliases. Apply the same storage allocation seam to Go type-checker metadata before constructing stored facts. Update OIPA's external publication adapter and every other `indexer.Publish` caller with the input contract; remove same-module hashed-ID construction rather than adding a legacy import path.

Preserve historical extraction keys, indexer-version labels and recorded input/export/configuration hashes as provenance of their original extraction. Migration changes representation, not those historical facts. Bump the Go indexer version and `ExternalIndexerVersion` for future publications; bump the external input/export encoding domains when their ID encoding changes. New facts therefore receive new extraction keys and cannot silently reuse an old input digest. This migration-only rewrite does not authorize later mutation under an unchanged extraction key.

### One shared projection builder

Implement the builder in storage so Go indexing, external `indexer.Publish` and historical backfill use identical validation and numbering. It accepts a format-4 document, its source revision, registered kinds and the referenced symbol rows keyed by bigint ID. Validate with `DecodeDocument`, verify all referenced rows and construct the two projection slices and status. Fetch symbol rows in batches rather than issuing one lookup per occurrence; no hash-to-handle translation remains on the publication or query path.

Encode declaration names/extents and occurrence sites through the shared compact-position codec. Validate a decoded record against its original `Range` and `ByteSpan` facts during backfill verification. Source text is unnecessary for this conversion; document coordinate arrays retain their exact values.

Inside the existing publication transaction, allocate identities and version ordinals, write document JSON, write locations/references/status, publish snapshot/source/package/symbol facts and advance the head with the existing version check. A failed validation, insert or head check rolls back every row. Reused documents reuse complete projections; forced extraction still rejects changed facts under an unchanged extraction key. Remove extracted-posting construction and posting writes after all readers have moved.

### Indexed readers

Derive active versions using effective sources and package input hashes. Query new indexes with the requested symbol IDs and kinds, constrained to those versions and admitted roots. Batch version sets and ID sets; reject stale versions in SQL rather than hydrating historical documents. Reuse existing scope, selector, limit, sorting and cache-invalidation behavior with the new ID encoding.

Storage readers decode `position` only for selected rows and return the existing typed ranges and byte spans. Keep line/column projection and comparison in the query layer after decoding; direct SQL line/column predicates are not part of this physical layout. Preserve the declaration source/start-offset index and exact-reference byte-span dedupe. Compact decoding performs no per-row source or document lookup.

| Consumer | New read path | JSON use |
| --- | --- | --- |
| Incoming references/callers | `symbol_references_to_idx`, kind and active-version filter | None for occurrences or positions |
| Callees/outgoing uses | `symbol_references_from_idx`, kind and active-version filter | None for occurrences or positions |
| Declarations/graph nodes | `symbol_locations_symbol_idx` plus source/scope metadata | Only when an existing result explicitly requires rich payloads |
| Implements/embeds/dispatch | Relationship rows, declaration locations and existing symbol metadata | None for relationship extraction |
| Exact `FindReferences` | Occurrence rows of kinds 0–5; preserve current position dedupe and definition precedence | None |
| Historical diff definition lookup | Location symbol IDs for selected versions | Saved document shapes/payloads still loaded when output needs them |
| Existence/fingerprints | Existing `DefinedSymbols`/`EffectiveSymbols` over symbol deltas | None |
| AST/explorer detail | Existing selected-document hydration | Retained JSON |

Graph symbol-node IDs change from hashes to decimal strings; structured identifiers, producer IDs and explorer identity keys are preserved. Join enclosing location facts for declaration text and definition lines. Preserve builtin handling, unresolved nodes, file-scope nodes, interface dispatch and read/write filters. Full-document JSON decode count for graph/reference/location-only queries must be zero. The graph still reads hash-verified source to derive guards and whole-call text.

Replace declaration lookup's positional parameters with an internal options surface carrying the requested IDs, result kind and whether payload hydration is required. Graph callers request location facts; existing rich result contracts request payloads. Keep public graph/reference response structures while updating their symbol-ID values and documented encoding; remove SHA-ID parsing and stale client assumptions.

### Pruning and removed interfaces

Replace `DefinitionPostings` and its same-module callers with declaration-location reads. Remove `SymbolPosting`, posting-role conversions, posting readers and writes. Do not retain a runtime compatibility path or a reconstructed postings view.

Delete projection/status children with dead documents, then remove unused revisions/blobs as before. Remove orphan-symbol deletion: every committed `symbols` row and owner mapping remains, even when all of its versions have been pruned. Preserve `symbol_modules`, `symbol_packages`, `symbol_kinds` and `symbol_layout`. Advance the existing prune epoch so caches detect removal of version facts. The permanent registry grows with all distinct symbols ever committed, rather than only the live corpus.

Replace `PruneResult.postings` with `references` and `symbol_locations` row counters, and remove the obsolete deleted-symbol counter. Rename `symbol_deltas.symbol_handle` to `symbol_id`, preserving integer values and its composite key; point its FK at `symbols.id`. Update generated API specifications, client types, CLI output and integration tests together when implementing. Remove automatic cleanup that drops identity mappings; index reset/reindex may clear only version facts while retaining the registry.

## Migration and rollout specification

Perform migration before serving queries, with writers quiesced and an exclusive migration lock. First exercise it on a consistent SQLite backup under `.tmp/`; a plain copy of only the live main file is insufficient when WAL contains committed data.

1. Verify the existing H64b layout, kind codes, namespace registries and one-to-one positive `symbols.handle` values. Inventory every text-ID field and FK. Reject duplicate natural keys, unknown IDs, dangling owners and packed-metadata disagreement before rewriting data. Databases without usable handles require a separately specified upgrade, not destructive generation discard.
2. In one schema-upgrade transaction, persist the old-text-ID-to-existing-handle map, build a complete shadow symbol table with the final bigint PK, resolved bigint owners and newly encoded natural keys, create its projection tables and add the metadata index. Commit the backfill-phase marker with that complete initialization so a restart cannot trust a partial registry. Retain the original symbol table until validation and cutover. These artifacts are private to the migration, not part of the final schema or runtime identity API. Do not ask the generic HCL diff engine to infer a populated text-to-bigint conversion; keep general destructive permissions disabled.
3. Scan retained documents by ordinal. A migration decoder reads the supported old format, replaces only its typed canonical-ID fields through the map and emits format 4. Validate against the shadow registry and write the upgraded JSON, locations, references and projection-version-1 status in one transaction per document. Bound input reads and insert batches within that transaction. Process empty and excluded documents too. Saved shapes, body hashes, producer payloads, explorer keys, declaration order and occurrence order must survive.
4. Resume using the recorded phase and each document's status. Only old-format documents with no projection state are pending. A complete format-4 document/status pair is validated and skipped; mismatched formats, partial rows, unsupported projection versions or conflicting counts are errors, never overwritten as missing work. Normal readers and publishers remain blocked during every intermediate phase. The migration decoder is the only old-format reader and is not a runtime fallback.
5. Validate every version's root/source ownership, counts, exact sites and translated IDs. Decode every compact position and compare its name/extent/occurrence ranges and byte spans with the saved document facts. Reconstruct old posting aggregates from resolved location IDs, occurrence targets and implements/embeds targets, applying existing builtin exclusions. Compare those grouped integer facts with saved postings. Verify all old symbol rows, including owners and otherwise unreferenced rows, exist in the shadow registry with precisely their former handle value. Preserve symbol-delta integers and fingerprints while staging their `symbol_id` rename/FK change.
6. Perform an engine-specific transactional final cutover: install the bigint table as `symbols`, retarget its self-FK and every dependent FK, install the delta column rename and final indexes/checks, then validate foreign keys and completeness. Drop the old symbol table, `symbol_postings` and all migration maps/state only after their consumers and checks have moved. Use an explicit dependency inventory and ordinary restrictions; never use `CASCADE` or silently drop unmanaged objects. The final schema has no hash-ID column, duplicate handle column, hash alias or temporary migration table.
7. Opening for queries succeeds only after final cutover commits. Verify reopening performs no document decoding/backfill for an up-to-date schema. `OpenReadOnly` reports an actionable migration error for missing tables, incomplete projections or an intermediate phase. An interrupted migration resumes under its lock; restore the consistent backup when abandoning it. A failed migration never serves a mixed schema or falls back to old posting/document queries.

Preserve all document IDs, ordinals, source revisions, existing handle values, snapshot IDs, base chains, dependency snapshots and location heads. Backfill retained history rather than discarding the index or requiring reindex. Document JSON changes only its symbol-ID representation and format as specified above; source bytes and recorded historical extraction provenance remain intact. The live endpoint is restarted onto the implemented schema only after backup validation; this documentation change does not restart it.

## Corpus and performance validation

Read-only measurements on `/Users/moshe/.config/uir/uir.db` on 2026-10-07 found 8,492 document versions, 182,237 declaration entries and 3,269,176 identifier occurrences. Initial projection cardinality is therefore 182,237 location rows and 3,269,176 occurrence rows plus declared implements/embeds rows, with 8,492 status rows. This is more rows than the aggregate posting design. Retained JSON plus new tables and indexes can increase total storage; measure that cost rather than predicting a smaller database.

### Compact-position findings, 2026-10-08

Read-only inspection of `file:/Users/moshe/.config/uir/uir.db?immutable=1` confirmed the same document, declaration and occurrence counts. The position distributions were:

| Observed fact | Declarations | Occurrences |
| --- | ---: | ---: |
| Entries | 182,237 | 3,269,176 |
| Single-line primary spans | 182,237 | 3,269,169 |
| Single-line spans with equal UTF-16 and byte lengths | 182,237 | 3,269,169 |
| Maximum primary end line | 2,488 | 2,568 |
| Maximum primary start/end UTF-16 column | 74 | 637 |
| Maximum primary byte end | 85,915 | 86,278 |

Only seven occurrences crossed lines. Declaration extents were single-line for 80,705 entries, so the codec must retain multiline extent facts even though every declaration name in this corpus fits on one line. The probes found no nonpositive line/column coordinates, nonpositive byte lengths or byte containment violations in the inspected name/extent/occurrence fields. This corpus supports the common-case encoding; it does not establish maximum supported coordinates or substitute for codec validation.

The following sizes were calculated from those saved facts using the format above. The expanded baseline is the earlier proposal's twelve declaration and six occurrence coordinate columns; these columns are not present in the current runtime schema.

| Modeled storage | Declarations | Occurrences | Total |
| --- | ---: | ---: | ---: |
| Binary-record payload bytes | 1,985,561 | 14,859,503 | 16,845,064 |
| Average binary-record bytes per entry | 10.895 | 4.545 | — |
| Expanded SQLite coordinate values and serial-type headers, bytes | 5,250,014 | 49,363,550 | 54,613,564 |
| Compact SQLite anchor/record values and serial-type headers, bytes | 2,714,058 | 27,947,513 | 30,661,571 |

The modeled coordinate component falls from **52.084 MiB to 29.241 MiB**, saving **22.842 MiB, or 43.86%**. The binary payload averages exclude the retained start-offset column and database headers; the SQLite component includes both values and their individual serial-type headers. Occurrence totals exclude the additional implements/embeds rows and all unrelated fields, primary/secondary indexes, record-length headers, B-tree cells, page overhead and retained document JSON. These are corpus-derived estimates for the two proposed layouts, not measured table sizes or a measured reduction of the existing database.

The measurement extracted the JSON coordinates once into a `MATERIALIZED` CTE, used connection-local `temp_store=MEMORY`, summed canonical unsigned/ZigZag varint widths and applied SQLite's [record-format integer widths and serial-type headers](https://sqlite.org/fileformat2.html#record_format). The shared-width flag omitted 182,237 declaration width bytes and 3,269,211 occurrence width bytes; a few long single-line occurrences needed more than one width byte before omission. Keeping extraction materialized avoids repeatedly parsing rich declaration JSON while evaluating the size formulas.

Packing a line/column pair into one bigint does not guarantee a smaller database representation. SQLite already stores small integers using value-dependent widths, and PostgreSQL's [two four-byte integers and one eight-byte bigint](https://www.postgresql.org/docs/current/datatype-numeric.html#DATATYPE-INT) have the same scalar payload size. The selected layout uses byte/column lengths and extent deltas to reduce repeated coordinates while keeping the start offset indexable. Current reference results require exact byte spans, and module/graph results consume line, column, end positions and declaration definition lines; retaining both coordinate systems avoids source-dependent reconstruction for historical navigation.

Implementation validation must measure actual SQLite and PostgreSQL table/index bytes, encode/decode throughput, allocation cost, publication/backfill time and the unchanged graph endpoint. Keep those future measurements separate from the estimates above.

### Existing graph and membership baseline

The graph review recorded the following baseline for the existing service, without changing its schema or clearing caches:

- Original unscoped `promptrun.Run`, `direction=both`, `depth=2`: 47 nodes, 55 edges, 14 snapshots and 477 SQL statements. Five subsequent serial requests had a 527.783 ms median; the initial unsaved request took 1.735555 s.
- The median response reported 342 ms of index membership and 175 ms of graph construction. SQL/GORM time overlaps those phases.
- A separate read-only metadata A–B–A probe over 2,877 Captain document versions reported 2,062 / 92,721 / 2,062 page-cache misses when ordinal was absent / included / absent, with zero page writes. This supports a payload-independent metadata access path; it is not an endpoint speedup prediction.

These are recorded observations, not current latency guarantees or established cold-cache measurements. The review evidence was captured under `.tmp/uir-graph-review/20261007T051151Z/` in the OIPA checkout. Retain the original URL, graph payload and measurement boundaries when comparing the implemented design:

```bash
curl --silent --show-error --max-time 60 --output /dev/null --write-out 'status=%{http_code} first_byte=%{time_starttransfer}s total=%{time_total}s bytes=%{size_download}\n' --url 'http://localhost:8071/api/v1/modules/graph?selector=github.com%2Fflanksource%2Fcaptain%2Fpkg%2Fpromptrun.Run&direction=both&depth=2' -H 'Accept: application/json'
```

Measure at least 30 serial requests on identical baseline and migrated backups, recording first-request latency separately from reused-process medians and percentiles. A fresh connection with a warm OS page cache is not a cold-cache test. Establish any cold-cache method explicitly. For comparison only, translate baseline hash IDs through the captured migration map to their unchanged numeric handles, including every graph endpoint and symbol-bearing field. Compare nodes, edges, sites, locations and guards after this ID normalization, ignoring timing stages only; the migrated service must not expose the old map. Do not narrow scope to Captain: that removed three Gavel nodes and changed the baseline result.

Report metadata and reference query plans, page reads, SQL count, decoded-document count, migration duration, peak memory, SQLite table/index bytes, WAL growth and fresh/reused publication times. Include permanent-registry growth over repeated add/delete/prune cycles and the cost of natural-key lookup/allocation. Test rare targets and high-fanout targets, several selected roots, historical versions, both graph directions and several depths. Benchmark the same fixtures on PostgreSQL. The graph still has source parsing costs, and the separate HTTP pipeline-lifetime issue remains outside this storage proposal.

## Implementation sequence and acceptance

1. Add focused failing Ginkgo cases for permanent natural-key lookup, owner resolution, bigint encoding, compact-position encoding and projection construction. Implement the sole-ID models, storage allocation seam, shared codec/builder and portable HCL using BYTEA/BLOB; schema assertions cover both engines and verify that the expanded coordinate columns are absent.
2. Add populated migration, interruption at each phase, read-only and idempotence cases. Implement typed document-ID rewrites, shadow-registry cutover, completeness validation and explicit removal of old hashes/postings/migration artifacts. Exercise SQLite and PostgreSQL foreign-key retargeting.
3. Move Go and external producers to storage-allocated IDs; update OIPA's publication adapter and other producers. Extend transactional publication tests for local-key resolution, reuse, forced mismatch and rollback. Bump future indexer/input/export format versions.
4. Move query/graph readers, selectors, exact references, diff definition reads, pruning and reindex. Update OpenAPI, generated clients, UI consumers and CLI/prune counters for decimal-string symbol IDs. Remove hashing/translation helpers, posting-only code and destructive identity-reset paths. Update the implemented symbol/indexer/storage documentation when runtime changes land.
5. Run focused storage/indexer/query/symboldiff cases, API integration tests, `make lint`, `make build` and the existing broader gates appropriate to implementation. Smoke the unchanged graph request against the migrated backup and compare the recorded payload after explicit baseline-ID normalization. Publish measured storage/write/read trade-offs.

Focused scenarios must cover IDs above JavaScript's `2^53` limit, invalid/overflow decimal strings, stable owner/member/overload IDs, changed publication-local keys, disappearance then prune/reindex/reappearance, reopening, unused retained registry rows, concurrent allocation and conflicting packed metadata. Migration cases include missing old-ID mappings, cycles, preserved source/provenance/fingerprints, interrupted phases, zero-fact documents, unsupported formats and absence of every final hash alias.

Projection/query scenarios cover repeated sites between the same symbols, equal offsets, definition/use overlap, file-level references, unresolved calls, builtins, non-ASCII positions, partial/syntax/excluded files, unproven declaration IDs, custom producer IDs/kinds, implements/embeds, dispatch, dependency retyping without byte changes, moved/deleted files, shared versions across snapshots/checkouts, stale-history exclusion, failed head advancement and corrupt projection status.

Keep pure codec tests separate from database integration tests. Use independently expected golden bytes, including the `41 03 6f 06` example, plus single/multiline names and extents, non-ASCII and astral Unicode widths, CRLF, offset zero, positive/negative/zero extent deltas, generated long lines, values beyond 32 bits and varint boundaries at 127/128 and 16,383/16,384. Corruption cases cover unsupported formats, reserved/inconsistent/wrong-table flags, truncated/overlong/noncanonical varints, overflow, trailing bytes, invalid endpoints and name/extent containment. Database cases cover raw BYTEA/BLOB round trips, preserved source-position index use and complete historical position equality. Compare expanded graph/reference results and require zero document hydration for position-only reads.

Acceptance for the future runtime change requires the existing handle as the sole ID everywhere, permanent committed mappings, complete historical backfill with preserved facts, no SHA symbol ID/alias or `symbol_postings` table/runtime fallback, exact compact-coordinate reconstruction, equivalent graph/reference behavior after baseline-ID normalization, zero full-document decodes for positional query paths and measured performance/cost results. The plan records the compact specification and estimates now; the next ERD revision must match the physical columns and be linted, rendered and visually reviewed as proposed before diagram completion is claimed.

## ERD rendering

The [TSX](uir-gorm-erd.tsx) is the tracked source. The generated HTML is a local preview ignored by Git. Render from the repository root; the template's first line is `// @live` so relationship arrows are measured and baked.

Pending compact-position synchronization: replace the location box's twelve coordinate rows with `name_start_offset bigint` and `position BYTEA/BLOB`, and replace the reference box's six coordinate rows with `start_offset bigint` and `position BYTEA/BLOB`. Label name/extent and occurrence reconstruction through format 1; retain every key, relationship and start-offset index. The existing expanded-coordinate render remains a previous revision until that update is rendered and reviewed.

```bash
facet lint docs/uir-gorm-erd.tsx
facet png docs/uir-gorm-erd.tsx --viewport 1600x1000 --output .tmp/symbol-reference-design/uir-gorm-erd.png
facet html --live docs/uir-gorm-erd.tsx --output docs/uir-gorm-erd.html
```

Review the full page and each section at readable resolution. Require legible fields, unambiguous nullable/cardinality labels, no box-crossing arrows, no clipped tables or index text and matching schema/index inventories. Use the diagram-designer skill's structured review in addition to inspecting the rendered images.
