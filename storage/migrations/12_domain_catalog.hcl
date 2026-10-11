table "symbol_property_schemas" {
  schema = schema.public
  column "name" {
    type = text
    null = false
  }
  column "schema" {
    type = jsonb
    null = false
  }
  primary_key { columns = [column.name] }
  foreign_key "symbol_property_schemas_kind_fkey" {
    columns = [column.name]
    ref_columns = [table.symbol_kinds.column.name]
    on_delete = CASCADE
    on_update = NO_ACTION
  }
}

table "relationship_kinds" {
  schema = schema.public
  column "name" {
    type = text
    null = false
  }
  column "type" {
    type = text
    null = false
  }
  column "schema" {
    type = jsonb
    null = false
  }
  primary_key { columns = [column.name] }
  check "relationship_kinds_name_check" { expr = "length(name) > 0" }
  check "relationship_kinds_type_check" { expr = "type IN ('import', 'call', 'reference', 'inheritance', 'implements', 'includes', 'foreign_key', 'read', 'write', 'dispatch')" }
}
