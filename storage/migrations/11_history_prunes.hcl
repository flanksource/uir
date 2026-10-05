// history_prunes counts, in its one row, the storage.PruneHistory runs that deleted rows. Pruning is the
// only way a published row disappears, and a deleted symbol may come back under another handle, so a
// reader that keeps symbols, documents, or snapshots across queries (query.Pipeline) reads the epoch and
// discards what it kept when the epoch moved. No row means nothing was ever pruned.
table "history_prunes" {
  schema = schema.public

  column "id" {
    type = integer
    null = false
  }
  column "epoch" {
    type = bigint
    null = false
  }

  primary_key { columns = [column.id] }
  check "history_prunes_id_check" { expr = "id = 1" }
  check "history_prunes_epoch_check" { expr = "epoch >= 1" }
}
