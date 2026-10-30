table "source_metrics" {
  schema = schema.public

  column "source_revision_id" { type = uuid }
  column "version" { type = integer }
  column "lines" { type = bigint }

  primary_key { columns = [column.source_revision_id, column.version] }
  foreign_key "source_metrics_revision_fkey" {
    columns = [column.source_revision_id]
    ref_columns = [table.source_revisions.column.id]
    on_delete = CASCADE
  }
  check "source_metrics_version_check" { expr = "version > 0" }
  check "source_metrics_lines_check" { expr = "lines >= 0" }
}

table "declaration_metrics" {
  schema = schema.public

  column "document_ordinal" { type = bigint }
  column "symbol_key" { type = text }
  column "symbol_handle" {
    type = bigint
    null = true
  }
  column "version" { type = integer }
  column "name_line" { type = integer }
  column "name_column" { type = integer }
  column "name_end_line" { type = integer }
  column "name_end_column" { type = integer }
  column "param_count" {
    type = integer
    null = true
  }
  column "body_lines" {
    type = integer
    null = true
  }

  primary_key { columns = [column.document_ordinal, column.symbol_key] }
  index "declaration_metrics_handle_idx" { columns = [column.symbol_handle, column.document_ordinal] }
  foreign_key "declaration_metrics_document_fkey" {
    columns = [column.document_ordinal]
    ref_columns = [table.documents.column.ordinal]
    on_delete = CASCADE
  }
  foreign_key "declaration_metrics_symbol_fkey" {
    columns = [column.symbol_handle]
    ref_columns = [table.symbols.column.handle]
    on_delete = NO_ACTION
  }
  check "declaration_metrics_key_check" { expr = "length(symbol_key) > 0" }
  check "declaration_metrics_version_check" { expr = "version > 0" }
  check "declaration_metrics_position_check" {
    expr = "(name_line > 0 AND name_column > 0 AND name_end_line >= name_line AND name_end_column > 0 AND (name_end_line > name_line OR name_end_column > name_column))"
  }
  check "declaration_metrics_counts_check" {
    expr = "((param_count IS NULL OR param_count >= 0) AND (body_lines IS NULL OR (param_count IS NOT NULL AND body_lines > 0)))"
  }
}
