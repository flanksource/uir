// symbol_modules numbers the module keys that H64b handles pack into their 11-bit module field. The
// empty key (builtins) is always 0 and "std" is always 1; every other key is allocated from 2.
table "symbol_modules" {
  schema = schema.public

  column "number" {
    type = integer
    null = false
  }
  column "module_key" {
    type = text
    null = false
  }

  primary_key { columns = [column.number] }
  unique "symbol_modules_module_key_key" { columns = [column.module_key] }
  check "symbol_modules_number_check" {
    expr = "((module_key = '' AND number = 0) OR (module_key = 'std' AND number = 1) OR (module_key NOT IN ('', 'std') AND number BETWEEN 2 AND 2047))"
  }
}

// symbol_packages numbers each module's package paths for the 16-bit package field.
table "symbol_packages" {
  schema = schema.public

  column "module_number" {
    type = integer
    null = false
  }
  column "number" {
    type = integer
    null = false
  }
  column "package_path" {
    type = text
    null = false
  }

  primary_key { columns = [column.module_number, column.number] }
  foreign_key "symbol_packages_module_fkey" {
    columns     = [column.module_number]
    ref_columns = [table.symbol_modules.column.number]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  unique "symbol_packages_module_number_package_path_key" { columns = [column.module_number, column.package_path] }
  check "symbol_packages_number_check" { expr = "number BETWEEN 0 AND 65535" }
}

// symbol_deltas records, per snapshot, how its defined-symbol set differs from its base's: `set` with
// the symbol's shape and body fingerprints (the first eight bytes of shape_hash and body_hash) when the
// symbol is new or changed, `delete` when the base defined it and the snapshot does not. Rows are keyed
// by the snapshot's and root's ordinals rather than their uuids.
table "symbol_deltas" {
  schema = schema.public

  column "snapshot_ordinal" {
    type = bigint
    null = false
  }
  column "root_ordinal" {
    type = integer
    null = false
  }
  column "symbol_handle" {
    type = bigint
    null = false
  }
  column "operation" {
    type = text
    null = false
  }
  column "shape_fp" {
    type = bigint
    null = true
  }
  column "body_fp" {
    type = bigint
    null = true
  }

  primary_key { columns = [column.snapshot_ordinal, column.symbol_handle] }
  foreign_key "symbol_deltas_snapshot_fkey" {
    columns     = [column.snapshot_ordinal]
    ref_columns = [table.snapshots.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "symbol_deltas_root_fkey" {
    columns     = [column.root_ordinal]
    ref_columns = [table.modules.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "symbol_deltas_symbol_fkey" {
    columns     = [column.symbol_handle]
    ref_columns = [table.symbols.column.handle]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  index "symbol_deltas_handle_idx" { columns = [column.symbol_handle, column.snapshot_ordinal] }
  check "symbol_deltas_operation_check" {
    expr = "((operation = 'set' AND shape_fp IS NOT NULL AND body_fp IS NOT NULL) OR (operation = 'delete' AND shape_fp IS NULL AND body_fp IS NULL))"
  }
}
