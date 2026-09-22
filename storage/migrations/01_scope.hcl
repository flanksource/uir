schema "public" {}

table "uir_projects" {
  schema = schema.public

  column "id" {
    null = false
    type = uuid
  }
  column "project_key" {
    null = false
    type = text
  }
  column "name" {
    null = false
    type = text
  }
  column "properties" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }
  column "created_at" {
    null = false
    type = timestamptz
  }
  column "updated_at" {
    null = false
    type = timestamptz
  }

  primary_key {
    columns = [column.id]
  }
  unique "uir_projects_project_key_key" {
    columns = [column.project_key]
  }
}

table "uir_snapshots" {
  schema = schema.public

  column "id" {
    null = false
    type = uuid
  }
  column "project_id" {
    null = false
    type = uuid
  }
  column "state" {
    null = false
    type = text
  }
  column "revision_set_hash" {
    null = false
    type = text
  }
  column "configuration_hash" {
    null = false
    type = text
  }
  column "extractor_version" {
    null = false
    type = text
  }
  column "payload_schema" {
    null = false
    type = text
  }
  column "document_payload" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }
  column "started_at" {
    null = false
    type = timestamptz
  }
  column "completed_at" {
    null = true
    type = timestamptz
  }
  column "properties" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "uir_snapshots_project_id_fkey" {
    columns     = [column.project_id]
    ref_columns = [table.uir_projects.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  unique "uir_snapshots_project_id_id_key" {
    columns = [column.project_id, column.id]
  }
  index "uir_snapshots_project_state_idx" {
    columns = [column.project_id, column.state]
  }
  index "uir_snapshots_revision_set_hash_idx" {
    columns = [column.revision_set_hash]
  }
  check "uir_snapshots_state_check" {
    expr = "state IN ('building', 'ready', 'failed')"
  }
}

table "uir_project_heads" {
  schema = schema.public

  column "project_id" {
    null = false
    type = uuid
  }
  column "snapshot_id" {
    null = false
    type = uuid
  }
  column "version" {
    null = false
    type = bigint
  }
  column "activated_at" {
    null = false
    type = timestamptz
  }

  primary_key {
    columns = [column.project_id]
  }
  foreign_key "uir_project_heads_project_id_fkey" {
    columns     = [column.project_id]
    ref_columns = [table.uir_projects.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "uir_project_heads_project_snapshot_fkey" {
    columns     = [column.project_id, column.snapshot_id]
    ref_columns = [table.uir_snapshots.column.project_id, table.uir_snapshots.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  unique "uir_project_heads_snapshot_id_key" {
    columns = [column.snapshot_id]
  }
  check "uir_project_heads_version_check" {
    expr = "version >= 0"
  }
}

table "uir_roots" {
  schema = schema.public

  column "id" {
    null = false
    type = uuid
  }
  column "snapshot_id" {
    null = false
    type = uuid
  }
  column "parent_root_id" {
    null = true
    type = uuid
  }
  column "root_key" {
    null = false
    type = text
  }
  column "kind" {
    null = false
    type = text
  }
  column "mount_path" {
    null = false
    type = text
  }
  column "repository_key" {
    null = true
    type = text
  }
  column "repository_uri" {
    null = true
    type = text
  }
  column "revision" {
    null = true
    type = text
  }
  column "content_set_hash" {
    null = false
    type = text
  }
  column "submodule_path" {
    null = true
    type = text
  }
  column "path_case" {
    null = false
    type = text
  }
  column "normalization_version" {
    null = false
    type = text
  }
  column "local_path" {
    null = true
    type = text
  }
  column "properties" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "uir_roots_snapshot_id_fkey" {
    columns     = [column.snapshot_id]
    ref_columns = [table.uir_snapshots.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "uir_roots_parent_root_fkey" {
    columns     = [column.snapshot_id, column.parent_root_id]
    ref_columns = [table.uir_roots.column.snapshot_id, table.uir_roots.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  unique "uir_roots_snapshot_id_id_key" {
    columns = [column.snapshot_id, column.id]
  }
  unique "uir_roots_snapshot_root_key" {
    columns = [column.snapshot_id, column.root_key]
  }
  unique "uir_roots_snapshot_mount_path_key" {
    columns = [column.snapshot_id, column.mount_path]
  }
  index "uir_roots_parent_root_id_idx" {
    columns = [column.parent_root_id]
  }
  index "uir_roots_repository_revision_idx" {
    columns = [column.repository_key, column.revision]
  }
  index "uir_roots_content_set_hash_idx" {
    columns = [column.content_set_hash]
  }
}

table "uir_sources" {
  schema = schema.public

  column "id" {
    null = false
    type = uuid
  }
  column "root_id" {
    null = false
    type = uuid
  }
  column "path_key" {
    null = false
    type = text
  }
  column "display_path" {
    null = false
    type = text
  }
  column "kind" {
    null = false
    type = text
  }
  column "language" {
    null = false
    type = text
  }
  column "content_hash" {
    null = false
    type = text
  }
  column "size_bytes" {
    null = false
    type = bigint
  }
  column "modified_at" {
    null = true
    type = timestamptz
  }
  column "properties" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "uir_sources_root_id_fkey" {
    columns     = [column.root_id]
    ref_columns = [table.uir_roots.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  unique "uir_sources_root_id_id_key" {
    columns = [column.root_id, column.id]
  }
  unique "uir_sources_root_path_key" {
    columns = [column.root_id, column.path_key]
  }
  index "uir_sources_kind_idx" {
    columns = [column.kind]
  }
  index "uir_sources_language_idx" {
    columns = [column.language]
  }
  index "uir_sources_content_hash_idx" {
    columns = [column.content_hash]
  }
  check "uir_sources_path_key_check" {
    expr = "path_key <> '' AND path_key NOT LIKE '/%' AND path_key NOT LIKE '../%' AND path_key NOT LIKE '%/../%'"
  }
}
