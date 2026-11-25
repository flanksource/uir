// import_graph_blobs holds the package import graphs a typed Go load read, one gzip-compressed JSON
// document per distinct graph, keyed by the SHA-256 of the uncompressed document. Snapshots that load
// the same dependency set and import the same packages share one blob.
table "import_graph_blobs" {
  schema = schema.public

  column "graph_hash" {
    type = text
    null = false
  }
  column "graph" {
    type = bytea
    null = false
  }

  primary_key { columns = [column.graph_hash] }
  check "import_graph_blobs_graph_hash_check" { expr = "length(graph_hash) = 64" }
}

// snapshot_import_graphs names, per Go snapshot, the import graph its typed load read: every
// non-standard-library package reachable from the root's non-test packages with its module and its
// imports, which package_imports() charges to each root package. A snapshot without a row was
// published before import graphs, or by a producer that has none, and a reader asks for a reindex.
table "snapshot_import_graphs" {
  schema = schema.public

  column "snapshot_id" {
    type = uuid
    null = false
  }
  column "graph_hash" {
    type = text
    null = false
  }

  primary_key { columns = [column.snapshot_id] }
  foreign_key "snapshot_import_graphs_snapshot_fkey" {
    columns     = [column.snapshot_id]
    ref_columns = [table.snapshots.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "snapshot_import_graphs_graph_fkey" {
    columns     = [column.graph_hash]
    ref_columns = [table.import_graph_blobs.column.graph_hash]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  index "snapshot_import_graphs_graph_idx" { columns = [column.graph_hash] }
}
