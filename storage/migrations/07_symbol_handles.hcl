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
