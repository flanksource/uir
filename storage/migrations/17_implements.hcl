// implements_deltas records, per snapshot, how its implements facts differ from its base's: `set` when
// a declared type of the snapshot satisfies an interface and its base's facts lack the pair, `delete`
// when the base's facts hold a pair the snapshot's do not. The facts are a snapshot-level pass over the
// whole typed load, not a per-document field, so a type is related to interfaces outside its import
// closure and a document stays a function of its package input hash. Rows are keyed by the snapshot's
// and root's ordinals, and by the handles of the type and the interface.
table "implements_deltas" {
  schema = schema.public

  column "snapshot_ordinal" {
    type = bigint
    null = false
  }
  column "root_ordinal" {
    type = integer
    null = false
  }
  column "type_handle" {
    type = bigint
    null = false
  }
  column "interface_handle" {
    type = bigint
    null = false
  }
  column "operation" {
    type = text
    null = false
  }

  primary_key { columns = [column.snapshot_ordinal, column.type_handle, column.interface_handle] }
  foreign_key "implements_deltas_snapshot_fkey" {
    columns     = [column.snapshot_ordinal]
    ref_columns = [table.snapshots.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "implements_deltas_root_fkey" {
    columns     = [column.root_ordinal]
    ref_columns = [table.modules.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "implements_deltas_type_fkey" {
    columns     = [column.type_handle]
    ref_columns = [table.symbols.column.handle]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  foreign_key "implements_deltas_interface_fkey" {
    columns     = [column.interface_handle]
    ref_columns = [table.symbols.column.handle]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  index "implements_deltas_interface_idx" { columns = [column.interface_handle, column.snapshot_ordinal] }
  index "implements_deltas_type_idx" { columns = [column.type_handle, column.snapshot_ordinal] }
  check "implements_deltas_operation_check" { expr = "operation IN ('set', 'delete')" }
}

// implements_facts marks a snapshot whose implements facts were computed, with how many pairs its
// effective facts hold. A snapshot without a row predates snapshot-level implements facts: its
// documents carried them instead, and a reader that needs them asks for a reindex.
table "implements_facts" {
  schema = schema.public

  column "snapshot_ordinal" {
    type = bigint
    null = false
  }
  column "root_ordinal" {
    type = integer
    null = false
  }
  column "pair_count" {
    type = bigint
    null = false
  }

  primary_key { columns = [column.snapshot_ordinal] }
  foreign_key "implements_facts_snapshot_fkey" {
    columns     = [column.snapshot_ordinal]
    ref_columns = [table.snapshots.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "implements_facts_root_fkey" {
    columns     = [column.root_ordinal]
    ref_columns = [table.modules.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  check "implements_facts_pair_count_check" { expr = "pair_count >= 0" }
}
