schema "public" {}

table "uir_module_roots" {
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
  unique "uir_module_roots_root_key_key" { columns = [column.root_key] }
  check "uir_module_roots_root_key_nonempty" { expr = "length(root_key) > 0" }
}

table "uir_module_locations" {
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
  foreign_key "uir_module_locations_root_id_fkey" {
    columns = [column.root_id]
    ref_columns = [table.uir_module_roots.column.id]
    on_update = NO_ACTION
    on_delete = CASCADE
  }
  foreign_key "uir_module_locations_parent_id_fkey" {
    columns = [column.parent_location_id]
    ref_columns = [table.uir_module_locations.column.id]
    on_update = NO_ACTION
    on_delete = NO_ACTION
  }
  unique "uir_module_locations_root_id_id_key" { columns = [column.root_id, column.id] }
  unique "uir_module_locations_root_path_key" { columns = [column.root_id, column.canonical_path] }
  check "uir_module_locations_path_nonempty" { expr = "length(canonical_path) > 0" }
}

table "uir_module_snapshots" {
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
  column "state" {
    type = text
    null = false
  }
  column "revision" {
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
  column "extractor_version" {
    type = text
    null = false
  }
  column "started_at" {
    type = timestamptz
    null = false
  }
  column "completed_at" {
    type = timestamptz
    null = true
  }

  primary_key { columns = [column.id] }
  foreign_key "uir_module_snapshots_root_id_fkey" {
    columns = [column.root_id]
    ref_columns = [table.uir_module_roots.column.id]
    on_update = NO_ACTION
    on_delete = CASCADE
  }
  foreign_key "uir_module_snapshots_location_fkey" {
    columns = [column.root_id, column.location_id]
    ref_columns = [table.uir_module_locations.column.root_id, table.uir_module_locations.column.id]
    on_update = NO_ACTION
    on_delete = CASCADE
  }
  foreign_key "uir_module_snapshots_base_fkey" {
    columns = [column.root_id, column.base_snapshot_id]
    ref_columns = [table.uir_module_snapshots.column.root_id, table.uir_module_snapshots.column.id]
    on_update = NO_ACTION
    on_delete = NO_ACTION
  }
  unique "uir_module_snapshots_root_id_id_key" { columns = [column.root_id, column.id] }
  index "uir_module_snapshots_location_started_idx" { columns = [column.location_id, column.started_at] }
  check "uir_module_snapshots_state_check" { expr = "state IN ('building', 'ready', 'failed')" }
}

table "uir_module_location_heads" {
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
  foreign_key "uir_module_location_heads_location_fkey" {
    columns = [column.root_id, column.location_id]
    ref_columns = [table.uir_module_locations.column.root_id, table.uir_module_locations.column.id]
    on_update = NO_ACTION
    on_delete = CASCADE
  }
  foreign_key "uir_module_location_heads_snapshot_fkey" {
    columns = [column.root_id, column.snapshot_id]
    ref_columns = [table.uir_module_snapshots.column.root_id, table.uir_module_snapshots.column.id]
    on_update = NO_ACTION
    on_delete = NO_ACTION
  }
  check "uir_module_location_heads_version_check" { expr = "version >= 1" }
}

table "uir_module_primaries" {
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
  foreign_key "uir_module_primaries_location_fkey" {
    columns = [column.root_id, column.location_id]
    ref_columns = [table.uir_module_locations.column.root_id, table.uir_module_locations.column.id]
    on_update = NO_ACTION
    on_delete = CASCADE
  }
}
