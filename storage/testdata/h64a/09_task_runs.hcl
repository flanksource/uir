// task_runs keeps the clicky task runs that finished, from any uir process, so `uir serve` lists CLI
// runs and runs from before a restart. Rows are written by storage/taskruns, one per run, replaced
// whenever the run is saved again.
table "task_runs" {
  schema = schema.public

  column "id" {
    type = text
    null = false
  }
  column "name" {
    type = text
    null = false
  }
  column "kind" {
    type = text
    null = false
  }
  column "status" {
    type = text
    null = false
  }
  column "labels" {
    type = jsonb
    null = false
  }
  column "started_at" {
    type = timestamptz
    null = true
  }
  column "finished_at" {
    type = timestamptz
    null = true
  }
  column "error" {
    type = text
    null = false
  }
  // The snapshot ids the run published, read from its group details.
  column "snapshot_ids" {
    type = jsonb
    null = false
  }
  // The listing summary (clicky RunMeta) and the bounded group and task snapshots a drill-down returns.
  column "meta" {
    type = jsonb
    null = false
  }
  column "snapshots" {
    type = jsonb
    null = false
  }
  column "saved_at" {
    type = timestamptz
    null = false
  }

  primary_key { columns = [column.id] }
  index "task_runs_kind_started_idx" { columns = [column.kind, column.started_at] }
  check "task_runs_id_check" { expr = "length(id) > 0" }
  check "task_runs_status_check" { expr = "length(status) > 0" }
}
