table "analysis_heads" {
  schema = schema.public
  column "location_id" { type = uuid }
  column "mode" { type = text }
  column "configuration_hash" { type = text }
  column "snapshot_id" { type = uuid }
  column "version" { type = bigint }
  column "inputs" { type = jsonb }
  primary_key { columns = [column.location_id, column.mode, column.configuration_hash] }
  foreign_key "analysis_heads_location_fkey" {
    columns = [column.location_id]
    ref_columns = [table.locations.column.id]
    on_delete = CASCADE
  }
  foreign_key "analysis_heads_snapshot_fkey" {
    columns = [column.location_id, column.snapshot_id]
    ref_columns = [table.snapshots.column.location_id, table.snapshots.column.id]
    on_delete = NO_ACTION
  }
  check "analysis_heads_mode_check" { expr = "mode IN ('syntax', 'typed')" }
  check "analysis_heads_version_check" { expr = "version > 0" }
}
