// document_deltas records, per snapshot, how its path → document membership differs from its base's:
// `set` binds the path to an immutable document, `delete` drops a path the base served. A snapshot
// without a base holds a set row for every path it serves. Readers take the newest row per path along
// the base chain (storage.ActiveDocuments), exactly as source deltas resolve revisions. The foreign keys
// tie the row's root to its snapshot's and the document to the row's root and path, so a document of
// another path or root cannot be bound.
table "document_deltas" {
  schema = schema.public

  column "snapshot_ordinal" {
    type = bigint
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
  column "document_ordinal" {
    type = bigint
    null = true
  }
  column "operation" {
    type = text
    null = false
  }

  primary_key { columns = [column.snapshot_ordinal, column.path_key] }
  foreign_key "document_deltas_snapshot_fkey" {
    columns     = [column.root_id, column.snapshot_ordinal]
    ref_columns = [table.snapshots.column.root_id, table.snapshots.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "document_deltas_document_fkey" {
    columns     = [column.root_id, column.path_key, column.document_ordinal]
    ref_columns = [table.documents.column.root_id, table.documents.column.path_key, table.documents.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  index "document_deltas_document_idx" { columns = [column.document_ordinal] }
  check "document_deltas_operation_check" {
    expr = "((operation = 'set' AND document_ordinal IS NOT NULL) OR (operation = 'delete' AND document_ordinal IS NULL))"
  }
}

// document_membership records, in its one row, the cutover of a database published before document
// deltas: every document's content digest and every snapshot's membership are backfilled in bounded
// batches, digested_through and members_through name the last document and snapshot ordinals done, and
// complete is set when both are. A database without the row has not started the cutover; a reader refuses
// a database whose row is incomplete.
table "document_membership" {
  schema = schema.public

  column "id" {
    type = integer
    null = false
  }
  column "digested_through" {
    type = bigint
    null = false
  }
  column "members_through" {
    type = bigint
    null = false
  }
  column "complete" {
    type = boolean
    null = false
  }

  primary_key { columns = [column.id] }
  check "document_membership_id_check" { expr = "id = 1" }
}
