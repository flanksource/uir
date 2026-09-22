table "uir_nodes" {
  schema = schema.public

  column "id" {
    null = false
    type = uuid
  }
  column "snapshot_id" {
    null = false
    type = uuid
  }
  column "root_id" {
    null = false
    type = uuid
  }
  column "parent_id" {
    null = true
    type = uuid
  }
  column "child_slot" {
    null = false
    type = text
  }
  column "ordinal" {
    null = false
    type = int
  }
  column "node_type" {
    null = false
    type = text
  }
  column "identity_key" {
    null = false
    type = text
  }
  column "symbol_key" {
    null = false
    type = text
  }
  column "module" {
    null = false
    type = text
  }
  column "package" {
    null = false
    type = text
  }
  column "type_name" {
    null = false
    type = text
  }
  column "method" {
    null = false
    type = text
  }
  column "field" {
    null = false
    type = text
  }
  column "signature" {
    null = false
    type = text
  }
  column "language" {
    null = false
    type = text
  }
  column "traits" {
    null    = false
    type    = jsonb
    default = sql("'[]'")
  }
  column "payload_schema" {
    null = false
    type = text
  }
  column "payload" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }
  column "semantic_hash" {
    null = false
    type = text
  }
  column "created_at" {
    null = false
    type = timestamptz
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "uir_nodes_snapshot_root_fkey" {
    columns     = [column.snapshot_id, column.root_id]
    ref_columns = [table.uir_roots.column.snapshot_id, table.uir_roots.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "uir_nodes_parent_fkey" {
    columns     = [column.root_id, column.parent_id]
    ref_columns = [table.uir_nodes.column.root_id, table.uir_nodes.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  unique "uir_nodes_snapshot_id_id_key" {
    columns = [column.snapshot_id, column.id]
  }
  unique "uir_nodes_root_id_id_key" {
    columns = [column.root_id, column.id]
  }
  unique "uir_nodes_snapshot_root_id_id_key" {
    columns = [column.snapshot_id, column.root_id, column.id]
  }
  unique "uir_nodes_root_identity_key" {
    columns = [column.root_id, column.identity_key]
  }
  index "uir_nodes_parent_slot_ordinal_idx" {
    columns = [column.parent_id, column.child_slot, column.ordinal]
  }
  index "uir_nodes_type_idx" {
    columns = [column.node_type]
  }
  index "uir_nodes_symbol_key_idx" {
    columns = [column.symbol_key]
  }
  index "uir_nodes_module_package_type_idx" {
    columns = [column.module, column.package, column.type_name]
  }
  index "uir_nodes_method_field_idx" {
    columns = [column.method, column.field]
  }
  index "uir_nodes_language_idx" {
    columns = [column.language]
  }
  index "uir_nodes_semantic_hash_idx" {
    columns = [column.semantic_hash]
  }
}

table "uir_node_locations" {
  schema = schema.public

  column "id" {
    null = false
    type = uuid
  }
  column "root_id" {
    null = false
    type = uuid
  }
  column "node_id" {
    null = false
    type = uuid
  }
  column "source_id" {
    null = false
    type = uuid
  }
  column "role" {
    null = false
    type = text
  }
  column "ordinal" {
    null = false
    type = int
  }
  column "start_line" {
    null = true
    type = int
  }
  column "end_line" {
    null = true
    type = int
  }
  column "column" {
    null = true
    type = int
  }
  column "is_primary" {
    null    = false
    type    = boolean
    default = false
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "uir_node_locations_node_fkey" {
    columns     = [column.root_id, column.node_id]
    ref_columns = [table.uir_nodes.column.root_id, table.uir_nodes.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "uir_node_locations_source_fkey" {
    columns     = [column.root_id, column.source_id]
    ref_columns = [table.uir_sources.column.root_id, table.uir_sources.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  unique "uir_node_locations_identity_key" {
    columns = [column.node_id, column.source_id, column.role, column.ordinal]
  }
  index "uir_node_locations_source_id_idx" {
    columns = [column.source_id]
  }
  index "uir_node_locations_role_idx" {
    columns = [column.role]
  }
  index "uir_node_locations_primary_key" {
    unique  = true
    columns = [column.node_id]
    where   = "is_primary"
  }
}

table "uir_fields" {
  schema = schema.public

  column "node_id" {
    null = false
    type = uuid
  }
  column "role" {
    null = false
    type = text
  }
  column "ordinal" {
    null = true
    type = int
  }
  column "label" {
    null = false
    type = text
  }
  column "field_type" {
    null = false
    type = text
  }
  column "native_type" {
    null = false
    type = text
  }
  column "max_length" {
    null = true
    type = int
  }
  column "precision" {
    null = true
    type = int
  }
  column "scale" {
    null = true
    type = int
  }
  column "enum_values" {
    null    = false
    type    = jsonb
    default = sql("'[]'")
  }
  column "type_ref" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }
  column "default_value" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }
  column "validation" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }
  column "visibility" {
    null = false
    type = text
  }
  column "is_nullable" {
    null    = false
    type    = boolean
    default = false
  }
  column "is_primary_key" {
    null    = false
    type    = boolean
    default = false
  }
  column "is_auto_increment" {
    null    = false
    type    = boolean
    default = false
  }
  column "is_unique" {
    null    = false
    type    = boolean
    default = false
  }
  column "is_read_only" {
    null    = false
    type    = boolean
    default = false
  }
  column "is_write_only" {
    null    = false
    type    = boolean
    default = false
  }

  primary_key {
    columns = [column.node_id]
  }
  foreign_key "uir_fields_node_id_fkey" {
    columns     = [column.node_id]
    ref_columns = [table.uir_nodes.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  index "uir_fields_role_idx" {
    columns = [column.role]
  }
  index "uir_fields_field_type_idx" {
    columns = [column.field_type]
  }
}
