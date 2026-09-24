table "symbols" {
  schema = schema.public

  column "id" {
    type = text
    null = false
  }
  column "identity_version" {
    type = integer
    null = false
  }
  column "canonical_key" {
    type = text
    null = false
  }
  column "module_key" {
    type = text
    null = false
  }
  column "package_path" {
    type = text
    null = false
  }
  column "kind" {
    type = text
    null = false
  }
  column "owner_id" {
    type = text
    null = true
  }
  column "name" {
    type = text
    null = false
  }
  column "search_name" {
    type = text
    null = false
  }
  column "visibility" {
    type = text
    null = false
  }
  column "parameter_types" {
    type = jsonb
    null = false
  }

  primary_key { columns = [column.id] }
  foreign_key "symbols_owner_id_fkey" {
    columns     = [column.owner_id]
    ref_columns = [table.symbols.column.id]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  unique "symbols_canonical_key_key" { columns = [column.canonical_key] }
  index "symbols_lookup_idx" { columns = [column.module_key, column.package_path, column.kind, column.owner_id, column.name] }
  index "symbols_search_idx" { columns = [column.search_name, column.module_key, column.id] }
  check "symbols_id_check" { expr = "length(id) = 64" }
  check "symbols_identity_version_check" { expr = "identity_version >= 1" }
  check "symbols_kind_check" { expr = "kind IN ('package', 'type', 'func', 'method', 'field', 'var', 'const', 'builtin')" }
  check "symbols_visibility_check" { expr = "visibility IN ('exported', 'internal')" }
  check "symbols_name_check" { expr = "length(name) > 0" }
  check "symbols_canonical_key_check" { expr = "length(canonical_key) > 0" }
  check "symbols_module_key_check" {
    expr = "((kind = 'builtin' AND length(module_key) = 0) OR (kind <> 'builtin' AND length(module_key) > 0))"
  }
}

table "documents" {
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
  column "source_revision_id" {
    type = uuid
    null = false
  }
  column "package_path" {
    type = text
    null = false
  }
  column "input_hash" {
    type = text
    null = false
  }
  column "indexer_version" {
    type = text
    null = false
  }
  column "coverage" {
    type = text
    null = false
  }
  column "symbol_count" {
    type = integer
    null = false
  }
  column "occurrence_count" {
    type = integer
    null = false
  }
  column "content" {
    type = jsonb
    null = false
  }

  primary_key { columns = [column.id] }
  foreign_key "documents_root_id_fkey" {
    columns     = [column.root_id]
    ref_columns = [table.modules.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "documents_source_revision_fkey" {
    columns     = [column.root_id, column.path_key, column.source_revision_id]
    ref_columns = [table.source_revisions.column.root_id, table.source_revisions.column.path_key, table.source_revisions.column.id]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  unique "documents_root_id_id_key" { columns = [column.root_id, column.id] }
  unique "documents_root_id_path_key_input_hash_key" { columns = [column.root_id, column.path_key, column.input_hash] }
  index "documents_source_idx" { columns = [column.source_revision_id] }
  check "documents_input_hash_check" { expr = "length(input_hash) = 64" }
  check "documents_path_key_check" { expr = "length(path_key) > 0" }
  check "documents_indexer_version_check" { expr = "length(indexer_version) > 0" }
  check "documents_coverage_check" { expr = "coverage IN ('indexed', 'partial', 'syntax', 'excluded')" }
  check "documents_symbol_count_check" { expr = "symbol_count >= 0" }
  check "documents_occurrence_count_check" { expr = "occurrence_count >= 0" }
  check "documents_excluded_check" { expr = "(coverage <> 'excluded' OR (symbol_count = 0 AND occurrence_count = 0))" }
}

table "symbol_postings" {
  schema = schema.public

  column "document_id" {
    type = uuid
    null = false
  }
  column "root_id" {
    type = uuid
    null = false
  }
  column "symbol_id" {
    type = text
    null = false
  }
  column "role" {
    type = text
    null = false
  }
  column "occurrence_count" {
    type = integer
    null = false
  }

  primary_key { columns = [column.document_id, column.symbol_id, column.role] }
  foreign_key "symbol_postings_document_fkey" {
    columns     = [column.root_id, column.document_id]
    ref_columns = [table.documents.column.root_id, table.documents.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "symbol_postings_symbol_id_fkey" {
    columns     = [column.symbol_id]
    ref_columns = [table.symbols.column.id]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  index "symbol_postings_symbol_idx" { columns = [column.symbol_id, column.role, column.root_id, column.document_id] }
  check "symbol_postings_role_check" { expr = "role IN ('definition', 'reference', 'implements')" }
  check "symbol_postings_occurrence_count_check" { expr = "occurrence_count >= 1" }
}

table "package_coverage" {
  schema = schema.public

  column "snapshot_id" {
    type = uuid
    null = false
  }
  column "root_id" {
    type = uuid
    null = false
  }
  column "package_path" {
    type = text
    null = false
  }
  column "input_hash" {
    type = text
    null = false
  }
  column "export_shape_hash" {
    type = text
    null = true
  }
  column "coverage" {
    type = text
    null = false
  }
  column "file_count" {
    type = integer
    null = false
  }
  column "diagnostics" {
    type = jsonb
    null = false
  }

  primary_key { columns = [column.snapshot_id, column.package_path] }
  foreign_key "package_coverage_snapshot_fkey" {
    columns     = [column.root_id, column.snapshot_id]
    ref_columns = [table.snapshots.column.root_id, table.snapshots.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  index "package_coverage_input_idx" { columns = [column.root_id, column.package_path, column.input_hash] }
  check "package_coverage_input_hash_check" { expr = "length(input_hash) = 64" }
  check "package_coverage_export_shape_hash_check" { expr = "(export_shape_hash IS NULL OR length(export_shape_hash) = 64)" }
  check "package_coverage_coverage_check" { expr = "coverage IN ('indexed', 'partial', 'syntax', 'excluded')" }
  check "package_coverage_indexed_check" { expr = "(coverage <> 'indexed' OR export_shape_hash IS NOT NULL)" }
  check "package_coverage_package_path_check" { expr = "length(package_path) > 0" }
  check "package_coverage_file_count_check" { expr = "file_count >= 0" }
}
