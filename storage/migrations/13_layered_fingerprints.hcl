// symbol_deltas records, per snapshot, how its defined-symbol set differs from its base's: `set` with
// the symbol's public, private, and content fingerprints (the first eight bytes of public_hash,
// private_hash, and content_hash) when the symbol is new or changed, `delete` when the base defined it
// and the snapshot does not. Rows are keyed by the snapshot's and root's ordinals rather than their
// uuids. It replaces the two-layer table 07 declared (shape_fp, body_fp): a database that still has
// that table is discarded by the fingerprint cutover before migration and reindexed.
table "symbol_deltas" {
  schema = schema.public

  column "snapshot_ordinal" {
    type = bigint
    null = false
  }
  column "root_ordinal" {
    type = integer
    null = false
  }
  column "symbol_handle" {
    type = bigint
    null = false
  }
  column "operation" {
    type = text
    null = false
  }
  column "public_fp" {
    type = bigint
    null = true
  }
  column "private_fp" {
    type = bigint
    null = true
  }
  column "content_fp" {
    type = bigint
    null = true
  }

  primary_key { columns = [column.snapshot_ordinal, column.symbol_handle] }
  foreign_key "symbol_deltas_snapshot_fkey" {
    columns     = [column.snapshot_ordinal]
    ref_columns = [table.snapshots.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "symbol_deltas_root_fkey" {
    columns     = [column.root_ordinal]
    ref_columns = [table.modules.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "symbol_deltas_symbol_fkey" {
    columns     = [column.symbol_handle]
    ref_columns = [table.symbols.column.handle]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  index "symbol_deltas_handle_idx" { columns = [column.symbol_handle, column.snapshot_ordinal] }
  check "symbol_deltas_operation_check" {
    expr = "((operation = 'set' AND public_fp IS NOT NULL AND private_fp IS NOT NULL AND content_fp IS NOT NULL) OR (operation = 'delete' AND public_fp IS NULL AND private_fp IS NULL AND content_fp IS NULL))"
  }
}
