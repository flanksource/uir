// symbol_generation_backfill records, in its one row, the cutover of a database published before
// symbol_deltas.content_generation: every snapshot's symbol deltas get their generation in ordinal
// order, one bounded transaction per snapshot, through_ordinal names the last snapshot ordinal done,
// and complete is set when all are. A base always has a lower ordinal than its dependant, so its
// generations are final when a dependant reads them. A database without the row has not started the
// cutover; a reader refuses a database whose row is incomplete.
table "symbol_generation_backfill" {
  schema = schema.public

  column "id" {
    type = integer
    null = false
  }
  column "through_ordinal" {
    type = bigint
    null = false
  }
  column "complete" {
    type = boolean
    null = false
  }

  primary_key { columns = [column.id] }
  check "symbol_generation_backfill_id_check" { expr = "id = 1" }
}
