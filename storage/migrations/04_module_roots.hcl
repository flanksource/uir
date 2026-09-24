schema "public" {}

table "modules" {
  schema = schema.public

  column "id" {
    type = uuid
    null = false
  }
  column "root_key" {
    type = text
    null = false
  }
  column "name" {
    type = text
    null = false
  }
  column "created_at" {
    type = timestamptz
    null = false
  }

  primary_key { columns = [column.id] }
  unique "modules_root_key_key" { columns = [column.root_key] }
  check "modules_root_key_check" { expr = "length(root_key) > 0" }
}

table "locations" {
  schema = schema.public

  column "id" {
    type = uuid
    null = false
  }
  column "root_id" {
    type = uuid
    null = false
  }
  column "canonical_path" {
    type = text
    null = false
  }
  column "parent_location_id" {
    type = uuid
    null = true
  }
  column "mount_path" {
    type = text
    null = false
  }
  column "kind" {
    type = text
    null = false
  }
  column "repository_uri" {
    type = text
    null = true
  }
  column "created_at" {
    type = timestamptz
    null = false
  }

  primary_key { columns = [column.id] }
  foreign_key "locations_root_id_fkey" {
    columns     = [column.root_id]
    ref_columns = [table.modules.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "locations_parent_location_id_fkey" {
    columns     = [column.parent_location_id]
    ref_columns = [table.locations.column.id]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  unique "locations_root_id_id_key" { columns = [column.root_id, column.id] }
  unique "locations_root_id_canonical_path_key" { columns = [column.root_id, column.canonical_path] }
  index "locations_parent_idx" { columns = [column.parent_location_id] }
  check "locations_canonical_path_check" { expr = "length(canonical_path) > 0" }
  check "locations_kind_check" { expr = "kind IN ('module', 'git', 'git-submodule')" }
}

table "snapshots" {
  schema = schema.public

  column "id" {
    type = uuid
    null = false
  }
  column "root_id" {
    type = uuid
    null = false
  }
  column "location_id" {
    type = uuid
    null = false
  }
  column "base_snapshot_id" {
    type = uuid
    null = true
  }
  column "revision" {
    type = text
    null = false
  }
  column "worktree_state" {
    type = text
    null = false
  }
  column "content_set_hash" {
    type = text
    null = false
  }
  column "configuration_hash" {
    type = text
    null = false
  }
  column "context_hash" {
    type = text
    null = false
  }
  column "coverage" {
    type = text
    null = false
  }
  column "package_count" {
    type = integer
    null = false
  }
  column "diagnostics" {
    type = jsonb
    null = false
  }
  column "started_at" {
    type = timestamptz
    null = false
  }
  column "completed_at" {
    type = timestamptz
    null = false
  }

  primary_key { columns = [column.id] }
  foreign_key "snapshots_root_id_fkey" {
    columns     = [column.root_id]
    ref_columns = [table.modules.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "snapshots_location_fkey" {
    columns     = [column.root_id, column.location_id]
    ref_columns = [table.locations.column.root_id, table.locations.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "snapshots_base_fkey" {
    columns     = [column.root_id, column.base_snapshot_id]
    ref_columns = [table.snapshots.column.root_id, table.snapshots.column.id]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  unique "snapshots_root_id_id_key" { columns = [column.root_id, column.id] }
  unique "snapshots_location_id_id_key" { columns = [column.location_id, column.id] }
  index "snapshots_location_started_idx" { columns = [column.location_id, column.started_at] }
  index "snapshots_base_idx" { columns = [column.base_snapshot_id] }
  index "snapshots_root_revision_idx" { columns = [column.root_id, column.revision] }
  check "snapshots_worktree_state_check" { expr = "worktree_state IN ('clean', 'dirty', 'unknown')" }
  check "snapshots_revision_check" { expr = "(worktree_state = 'unknown') OR length(revision) > 0" }
  check "snapshots_context_hash_check" { expr = "length(context_hash) = 64" }
  check "snapshots_coverage_check" { expr = "coverage IN ('indexed', 'partial', 'syntax', 'excluded')" }
  check "snapshots_package_count_check" { expr = "package_count >= 0" }
}

table "location_heads" {
  schema = schema.public

  column "root_id" {
    type = uuid
    null = false
  }
  column "location_id" {
    type = uuid
    null = false
  }
  column "snapshot_id" {
    type = uuid
    null = false
  }
  column "version" {
    type = bigint
    null = false
  }

  primary_key { columns = [column.location_id] }
  foreign_key "location_heads_location_fkey" {
    columns     = [column.root_id, column.location_id]
    ref_columns = [table.locations.column.root_id, table.locations.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "location_heads_snapshot_fkey" {
    columns     = [column.location_id, column.snapshot_id]
    ref_columns = [table.snapshots.column.location_id, table.snapshots.column.id]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  check "location_heads_version_check" { expr = "version >= 1" }
}

table "primary_locations" {
  schema = schema.public

  column "root_id" {
    type = uuid
    null = false
  }
  column "location_id" {
    type = uuid
    null = false
  }

  primary_key { columns = [column.root_id] }
  foreign_key "primary_locations_location_fkey" {
    columns     = [column.root_id, column.location_id]
    ref_columns = [table.locations.column.root_id, table.locations.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
}
