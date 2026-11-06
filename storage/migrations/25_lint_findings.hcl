// lint_findings holds what one lint rule found for one subject, a derived fact reused while the subject's
// closure, the facts the rule read for it, is unchanged. A subject is a symbol (subject_handle, with
// subject_key ''), a file (subject_handle 0, subject_key '<root id>:<path>') or a whole query scope
// (subject_handle 0, subject_key 'scope:<digest>'). rule_version digests the rule's query, message template
// and declared reads. closure_fingerprint digests the closure at the snapshot the row was produced from, and
// closure_source says what it holds: subject (the symbol at the layer the rule reads, and its declaring
// documents when the rule reads document facts), unselected (only the symbol's identity: the rule's
// source does not select it), file (the file's document) or snapshot (every snapshot of the scope).
// findings is the JSON array of findings, empty for a subject the rule passed, so a clean result is reused
// too. A row is reused only while its rule version and closure fingerprint match the snapshot being
// linted and expires_at, which a producer whose findings read inputs outside UIR sets, has not passed.
// document_id is the subject's declaring document when the row was produced, so a reused finding whose
// declaration moved is relocated, and content_generation the subject's content generation then. Closures
// and rows name documents by id, never by ordinal: pruning frees ordinals a later document can be given.
// Writing a pair replaces its row. subject_handle has no foreign key: pruning a symbol deletes its rows.
table "lint_findings" {
  schema = schema.public

  column "rule_id" { type = text }
  column "rule_version" { type = text }
  column "subject_handle" { type = bigint }
  column "subject_key" { type = text }
  column "closure_source" { type = text }
  column "closure_fingerprint" { type = text }
  column "content_generation" {
    type = bigint
    null = true
  }
  column "document_id" {
    type = uuid
    null = true
  }
  column "findings" { type = jsonb }
  column "produced_at" { type = timestamptz }
  column "expires_at" {
    type = timestamptz
    null = true
  }

  primary_key { columns = [column.rule_id, column.subject_handle, column.subject_key] }
  check "lint_findings_subject_check" { expr = "((subject_handle > 0 AND subject_key = '') OR (subject_handle = 0 AND length(subject_key) > 0))" }
  check "lint_findings_closure_check" { expr = "length(closure_fingerprint) = 64 AND length(rule_version) = 64 AND closure_source IN ('subject', 'unselected', 'file', 'snapshot')" }
}
