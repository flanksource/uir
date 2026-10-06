table "snapshot_dependencies" {
  schema = schema.public

  column "snapshot_id" {
    type = uuid
    null = false
  }
  column "module_path" {
    type = text
    null = false
  }
  column "declared_version" {
    type = text
    null = false
  }
  column "indirect" {
    type = boolean
    null = false
  }
  column "replace_path" {
    type = text
    null = false
  }
  column "replace_version" {
    type = text
    null = false
  }
  column "selected_version" {
    type = text
    null = false
  }
  column "target_snapshot_id" {
    type = uuid
    null = true
  }
  column "unresolved_reason" {
    type = text
    null = false
  }

  primary_key { columns = [column.snapshot_id, column.module_path] }
  foreign_key "snapshot_dependencies_source_fkey" {
    columns = [column.snapshot_id]
    ref_columns = [table.snapshots.column.id]
    on_update = NO_ACTION
    on_delete = CASCADE
  }
  foreign_key "snapshot_dependencies_target_fkey" {
    columns = [column.target_snapshot_id]
    ref_columns = [table.snapshots.column.id]
    on_update = NO_ACTION
    on_delete = NO_ACTION
  }
  index "snapshot_dependencies_target_idx" { columns = [column.target_snapshot_id] }
  check "snapshot_dependencies_module_path_check" { expr = "length(module_path) > 0" }
  check "snapshot_dependencies_target_or_reason_check" { expr = "((target_snapshot_id IS NOT NULL AND unresolved_reason = '') OR (target_snapshot_id IS NULL AND length(unresolved_reason) > 0))" }
}

table "source_blobs" {
  schema = schema.public
  column "content_hash" {
    type = text
    null = false
  }
  column "content" {
    type = bytea
    null = false
  }
  primary_key { columns = [column.content_hash] }
  check "source_blobs_content_hash_check" { expr = "length(content_hash) = 64" }
}
