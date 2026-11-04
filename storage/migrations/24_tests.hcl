// test_cases names each test a run reported for a root: its framework, package and length-delimited
// suite path plus name (name_path), and display, the same segments joined for people. symbol_handle
// links the test to the declaration a snapshot proved it is: a Go TestX func, or the innermost proven
// declaration enclosing the reported file and line. Pruning counts it as a reference to its symbol.
table "test_cases" {
  schema = schema.public

  column "id" { type = uuid }
  column "root_id" { type = uuid }
  column "framework" { type = text }
  column "package_path" { type = text }
  column "name_path" { type = text }
  column "display" { type = text }
  column "symbol_handle" {
    type = bigint
    null = true
  }

  primary_key { columns = [column.id] }
  unique "test_cases_name_key" { columns = [column.root_id, column.framework, column.package_path, column.name_path] }
  index "test_cases_symbol_idx" { columns = [column.symbol_handle] }
  foreign_key "test_cases_root_fkey" {
    columns     = [column.root_id]
    ref_columns = [table.modules.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "test_cases_symbol_fkey" {
    columns     = [column.symbol_handle]
    ref_columns = [table.symbols.column.handle]
    on_update   = NO_ACTION
    on_delete   = NO_ACTION
  }
  check "test_cases_name_check" { expr = "length(framework) > 0 AND length(name_path) > 0" }
}

// test_runs records one execution of a root's tests against the snapshot whose bytes it executed, which
// the import proved current. environment_hash digests the producer-reported environment (GOOS, GOARCH,
// toolchain, -race, tags, -count). A pruned snapshot leaves its runs with a null snapshot_id: their
// results keep the closure fingerprints flakiness compares.
table "test_runs" {
  schema = schema.public

  column "id" { type = uuid }
  column "root_id" { type = uuid }
  column "snapshot_id" {
    type = uuid
    null = true
  }
  column "producer" { type = text }
  column "environment" { type = jsonb }
  column "environment_hash" { type = text }
  column "started_at" { type = timestamptz }
  column "finished_at" { type = timestamptz }
  column "task_run_id" {
    type = text
    null = true
  }

  primary_key { columns = [column.id] }
  index "test_runs_root_idx" { columns = [column.root_id, column.started_at] }
  index "test_runs_snapshot_idx" { columns = [column.snapshot_id] }
  foreign_key "test_runs_root_fkey" {
    columns     = [column.root_id]
    ref_columns = [table.modules.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "test_runs_snapshot_fkey" {
    columns     = [column.snapshot_id]
    ref_columns = [table.snapshots.column.id]
    on_update   = NO_ACTION
    on_delete   = SET_NULL
  }
  check "test_runs_environment_hash_check" { expr = "length(environment_hash) = 64" }
}

// test_results holds one row per test, run and attempt. closure_fingerprint digests the content
// fingerprints of the symbols the test depends on, and closure_source says how that set was found:
// coverage (symbols the test's own coverage executed), static (its complete call graph) or snapshot
// (the whole snapshot). Flakiness only compares results of one test, closure source, closure fingerprint
// and environment.
table "test_results" {
  schema = schema.public

  column "run_id" { type = uuid }
  column "test_case_id" { type = uuid }
  column "attempt" { type = integer }
  column "outcome" { type = text }
  column "duration_ms" { type = bigint }
  column "failure_digest" {
    type = text
    null = true
  }
  column "closure_fingerprint" { type = text }
  column "closure_source" { type = text }

  primary_key { columns = [column.run_id, column.test_case_id, column.attempt] }
  index "test_results_closure_idx" { columns = [column.test_case_id, column.closure_fingerprint] }
  foreign_key "test_results_run_fkey" {
    columns     = [column.run_id]
    ref_columns = [table.test_runs.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "test_results_case_fkey" {
    columns     = [column.test_case_id]
    ref_columns = [table.test_cases.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  check "test_results_attempt_check" { expr = "attempt >= 1 AND duration_ms >= 0" }
  check "test_results_outcome_check" { expr = "outcome IN ('pass', 'fail', 'skip', 'timeout', 'aborted')" }
  check "test_results_closure_check" { expr = "length(closure_fingerprint) = 64 AND closure_source IN ('coverage', 'static', 'snapshot')" }
}

// test_coverage holds, per run, the statements a run (test_case_id null) or one test executed in one
// immutable document, per proven declaration: symbol_handle 0, which no symbol has, is the file scope.
// content_fp and content_generation are the declaration's at the run's snapshot, so coverage is reused
// on a later snapshot only while both still hold. symbol_handle has no foreign key: the document's own
// definition posting keeps its symbol, and the row cascades with the document.
table "test_coverage" {
  schema = schema.public

  column "run_id" { type = uuid }
  column "test_case_id" {
    type = uuid
    null = true
  }
  column "document_ordinal" { type = bigint }
  column "symbol_handle" { type = bigint }
  column "statements" { type = integer }
  column "covered" { type = integer }
  column "content_fp" {
    type = bigint
    null = true
  }
  column "content_generation" {
    type = bigint
    null = true
  }

  index "test_coverage_run_idx" { columns = [column.run_id, column.test_case_id] }
  index "test_coverage_symbol_idx" { columns = [column.symbol_handle, column.document_ordinal] }
  foreign_key "test_coverage_run_fkey" {
    columns     = [column.run_id]
    ref_columns = [table.test_runs.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "test_coverage_case_fkey" {
    columns     = [column.test_case_id]
    ref_columns = [table.test_cases.column.id]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  foreign_key "test_coverage_document_fkey" {
    columns     = [column.document_ordinal]
    ref_columns = [table.documents.column.ordinal]
    on_update   = NO_ACTION
    on_delete   = CASCADE
  }
  check "test_coverage_counts_check" { expr = "statements >= 0 AND covered >= 0 AND covered <= statements AND symbol_handle >= 0" }
  check "test_coverage_symbol_check" { expr = "((symbol_handle = 0 AND content_fp IS NULL) OR (symbol_handle > 0 AND content_fp IS NOT NULL))" }
}
