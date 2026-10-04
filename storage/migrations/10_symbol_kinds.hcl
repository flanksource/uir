// symbol_kinds is the kind registry that H64b handles pack into their 6-bit kind field, and that
// symbols.kind references by name. Code 0 is reserved. The builtin kinds (storage/symbolhandle) hold
// codes upward from 1 with an empty namespace, and opening the database inserts them; a custom kind is
// namespaced (`oipa.rule`) and is allocated by storage.RegisterKind from 63 downward.
table "symbol_kinds" {
  schema = schema.public

  column "code" {
    type = smallint
    null = false
  }
  column "name" {
    type = text
    null = false
  }
  column "category" {
    type = text
    null = false
  }
  column "namespace" {
    type = text
    null = false
  }

  primary_key { columns = [column.code] }
  unique "symbol_kinds_name_key" { columns = [column.name] }
  check "symbol_kinds_code_check" { expr = "code BETWEEN 1 AND 63" }
  check "symbol_kinds_name_check" { expr = "length(name) > 0" }
  check "symbol_kinds_category_check" { expr = "category IN ('container', 'type', 'callable', 'member')" }
}

// symbol_layout records, in its one row, the bit layout the database's symbol handles were packed with
// (symbolhandle.Layout). A database without it predates H64b; opening it discards the index.
table "symbol_layout" {
  schema = schema.public

  column "id" {
    type = integer
    null = false
  }
  column "layout" {
    type = text
    null = false
  }

  primary_key { columns = [column.id] }
  check "symbol_layout_id_check" { expr = "id = 1" }
  check "symbol_layout_layout_check" { expr = "length(layout) > 0" }
}
