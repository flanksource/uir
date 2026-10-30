// symbol_kind_metrics lists the integer metrics a producer declares for a registered kind. A kind may
// gain metrics on re-registration; a declared metric is never removed or redescribed.
table "symbol_kind_metrics" {
  schema = schema.public

  column "kind" { type = text }
  column "name" { type = text }
  column "description" { type = text }

  primary_key { columns = [column.kind, column.name] }
  foreign_key "symbol_kind_metrics_kind_fkey" {
    columns = [column.kind]
    ref_columns = [table.symbol_kinds.column.name]
    on_delete = CASCADE
    on_update = NO_ACTION
  }
  check "symbol_kind_metrics_name_check" { expr = "length(name) > 0" }
}

// declaration_measures holds one row per producer metric value of a declaration in an immutable
// document, beside its declaration_metrics row.
table "declaration_measures" {
  schema = schema.public

  column "document_ordinal" { type = bigint }
  column "symbol_key" { type = text }
  column "name" { type = text }
  column "symbol_handle" { type = bigint }
  column "value" { type = bigint }

  primary_key { columns = [column.document_ordinal, column.symbol_key, column.name] }
  index "declaration_measures_handle_idx" { columns = [column.symbol_handle, column.document_ordinal] }
  foreign_key "declaration_measures_document_fkey" {
    columns = [column.document_ordinal]
    ref_columns = [table.documents.column.ordinal]
    on_delete = CASCADE
  }
  foreign_key "declaration_measures_symbol_fkey" {
    columns = [column.symbol_handle]
    ref_columns = [table.symbols.column.handle]
    on_delete = NO_ACTION
  }
  check "declaration_measures_key_check" { expr = "length(symbol_key) > 0 AND length(name) > 0" }
  check "declaration_measures_value_check" { expr = "value >= 0" }
}
