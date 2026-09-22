table "uir_relationships" {
  schema = schema.public

  column "id" {
    null = false
    type = uuid
  }
  column "snapshot_id" {
    null = false
    type = uuid
  }
  column "from_root_id" {
    null = false
    type = uuid
  }
  column "from_node_id" {
    null = false
    type = uuid
  }
  column "to_snapshot_id" {
    null = true
    type = uuid
  }
  column "to_node_id" {
    null = true
    type = uuid
  }
  column "edge_key" {
    null = false
    type = text
  }
  column "to_project_key" {
    null = true
    type = text
  }
  column "to_root_key" {
    null = true
    type = text
  }
  column "to_identity_key" {
    null = false
    type = text
  }
  column "to_symbol_key" {
    null = false
    type = text
  }
  column "to_identifier" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }
  column "relationship_type" {
    null = false
    type = text
  }
  column "source_id" {
    null = true
    type = uuid
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
  column "statement_path" {
    null = false
    type = text
  }
  column "text" {
    null = false
    type = text
  }
  column "payload" {
    null    = false
    type    = jsonb
    default = sql("'{}'")
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "uir_relationships_from_node_fkey" {
    columns     = [column.snapshot_id, column.from_root_id, column.from_node_id]
    ref_columns = [table.uir_nodes.column.snapshot_id, table.uir_nodes.column.root_id, table.uir_nodes.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "uir_relationships_source_fkey" {
    columns     = [column.from_root_id, column.source_id]
    ref_columns = [table.uir_sources.column.root_id, table.uir_sources.column.id]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  foreign_key "uir_relationships_to_node_fkey" {
    columns     = [column.to_snapshot_id, column.to_node_id]
    ref_columns = [table.uir_nodes.column.snapshot_id, table.uir_nodes.column.id]
    on_update   = NO_ACTION
    on_delete   = SET_NULL
  }
  unique "uir_relationships_from_edge_key" {
    columns = [column.from_node_id, column.edge_key]
  }
  index "uir_relationships_snapshot_id_idx" {
    columns = [column.snapshot_id]
  }
  index "uir_relationships_from_root_id_idx" {
    columns = [column.from_root_id]
  }
  index "uir_relationships_to_node_id_idx" {
    columns = [column.to_node_id]
  }
  index "uir_relationships_target_locator_idx" {
    columns = [column.to_project_key, column.to_root_key, column.to_identity_key]
  }
  index "uir_relationships_to_symbol_key_idx" {
    columns = [column.to_symbol_key]
  }
  index "uir_relationships_type_idx" {
    columns = [column.relationship_type]
  }
  index "uir_relationships_source_id_idx" {
    columns = [column.source_id]
  }
  check "uir_relationships_target_pair_check" {
    expr = "((to_snapshot_id IS NULL AND to_node_id IS NULL) OR (to_snapshot_id = snapshot_id AND to_node_id IS NOT NULL))"
  }
  check "uir_relationships_identity_key_check" {
    expr = "to_identity_key <> ''"
  }
}
