table "uir_source_revisions" {
  schema = schema.public

  column "id" {
    type = uuid
    null = false
  }
  column "root_id" {
    type = uuid
    null = false
  }
  column "path_key" {
    type = text
    null = false
  }
  column "content_hash" {
    type = text
    null = false
  }
  column "package_path" {
    type = text
    null = false
  }
  column "extractor_version" {
    type = text
    null = false
  }
  column "size_bytes" {
    type = bigint
    null = false
  }
  column "projection" {
    type = jsonb
    null = false
  }

  primary_key { columns = [column.id] }
  foreign_key "uir_source_revisions_root_id_fkey" {
    columns = [column.root_id]
    ref_columns = [table.uir_module_roots.column.id]
    on_update = NO_ACTION
    on_delete = CASCADE
  }
  unique "uir_source_revisions_root_id_id_key" { columns = [column.root_id, column.id] }
  unique "uir_source_revisions_content_key" {
    columns = [column.root_id, column.path_key, column.content_hash, column.package_path, column.extractor_version]
  }
  check "uir_source_revisions_path_nonempty" { expr = "length(path_key) > 0" }
  check "uir_source_revisions_size_check" { expr = "size_bytes >= 0" }
}

table "uir_source_deltas" {
  schema = schema.public

  column "snapshot_id" {
    type = uuid
    null = false
  }
  column "root_id" {
    type = uuid
    null = false
  }
  column "path_key" {
    type = text
    null = false
  }
  column "revision_id" {
    type = uuid
    null = true
  }
  column "operation" {
    type = text
    null = false
  }

  primary_key { columns = [column.snapshot_id, column.path_key] }
  foreign_key "uir_source_deltas_snapshot_fkey" {
    columns = [column.root_id, column.snapshot_id]
    ref_columns = [table.uir_module_snapshots.column.root_id, table.uir_module_snapshots.column.id]
    on_update = NO_ACTION
    on_delete = CASCADE
  }
  foreign_key "uir_source_deltas_revision_fkey" {
    columns = [column.root_id, column.revision_id]
    ref_columns = [table.uir_source_revisions.column.root_id, table.uir_source_revisions.column.id]
    on_update = NO_ACTION
    on_delete = NO_ACTION
  }
  index "uir_source_deltas_revision_id_idx" { columns = [column.revision_id] }
  check "uir_source_deltas_operation_check" {
    expr = "((operation = 'set' AND revision_id IS NOT NULL) OR (operation = 'delete' AND revision_id IS NULL))"
  }
}
