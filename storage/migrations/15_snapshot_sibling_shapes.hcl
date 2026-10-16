// snapshot_sibling_shapes records, per Go snapshot, the workspace sibling packages its typed load read:
// packages loaded from disk that belong to another module than the snapshot's. A package the snapshot
// imports directly carries the export shape it presented; every package of the transitive closure
// carries a digest of its sources as the load read them (its module's go.mod and its files' path keys
// and content hashes). A direct import written only in files no variant compiled has neither. A head
// is reused without type-checking when every sibling still presents its recorded shape or, for a
// sibling indexed under another go.work, when every recorded source digest is unchanged on disk; a
// head without rows that imports a sibling is always type-checked.
table "snapshot_sibling_shapes" {
  schema = schema.public

  column "snapshot_id" {
    type = uuid
    null = false
  }
  column "import_path" {
    type = text
    null = false
  }
  column "export_shape_hash" {
    type = text
    null = true
  }
  column "source_digest" {
    type = text
    null = true
  }

  primary_key { columns = [column.snapshot_id, column.import_path] }
  foreign_key "snapshot_sibling_shapes_snapshot_fkey" {
    columns     = [column.snapshot_id]
    ref_columns = [table.snapshots.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  check "snapshot_sibling_shapes_import_path_check" { expr = "length(import_path) > 0" }
  check "snapshot_sibling_shapes_export_shape_hash_check" { expr = "(export_shape_hash IS NULL OR length(export_shape_hash) = 64)" }
  check "snapshot_sibling_shapes_source_digest_check" { expr = "(source_digest IS NULL OR length(source_digest) = 64)" }
}
