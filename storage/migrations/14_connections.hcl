table "connections" {
  schema = schema.public
  column "id" {
    type = uuid
    null = false
  }
  column "name" {
    type = text
    null = false
  }
  column "namespace" {
    type = text
    null = false
  }
  column "type" {
    type = text
    null = false
  }
  column "url" {
    type = text
    null = false
  }
  column "username" {
    type = text
    null = false
  }
  column "password" {
    type = text
    null = false
  }
  column "certificate" {
    type = text
    null = false
  }
  column "properties" {
    type = jsonb
    null = true
  }
  column "insecure_tls" {
    type = boolean
    null = false
  }
  column "created_at" {
    type = timestamptz
    null = false
  }
  column "updated_at" {
    type = timestamptz
    null = false
  }
  primary_key { columns = [column.id] }
  unique "connections_namespace_name_key" { columns = [column.namespace, column.name] }
  check "connections_name_check" { expr = "length(name) > 0" }
  check "connections_type_check" { expr = "type IN ('git', 'github', 'azure_devops', 'gitlab', 'postgres', 'sql_server', 'sqlite', 's3', 'mysql')" }
}

table "database_modules" {
  schema = schema.public
  column "id" {
    type = uuid
    null = false
  }
  column "connection_id" {
    type = uuid
    null = false
  }
  column "database_name" {
    type = text
    null = false
  }
  column "name" {
    type = text
    null = false
  }
  column "root_key" {
    type = text
    null = false
  }
  column "created_at" {
    type = timestamptz
    null = false
  }
  primary_key { columns = [column.id] }
  foreign_key "database_modules_connection_id_fkey" {
    columns = [column.connection_id]
    ref_columns = [table.connections.column.id]
    on_update = NO_ACTION
    on_delete = NO_ACTION
  }
  unique "database_modules_connection_database_key" { columns = [column.connection_id, column.database_name] }
  unique "database_modules_root_key_key" { columns = [column.root_key] }
  check "database_modules_database_name_check" { expr = "length(database_name) > 0" }
  check "database_modules_name_check" { expr = "length(name) > 0" }
}
