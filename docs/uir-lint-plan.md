# SQL lint implementation plan

Status: implemented locally; release and full acceptance gates remain pending.

Add a local `uir lint` command driven by the nearest `.uir.yaml`, stopping discovery at the Git root. An explicit `--config` selects a single configuration; its directory is the project root. There is no inheritance.

The version 1 configuration contains `lint.include_tests` and SQL rules with unique IDs, severity (error by default), query and Go template message. Every returned row is a finding. Reject unknown fields, duplicate keys, unsupported versions, invalid severities, empty rules, explicit SQL LIMIT and terminal graph/path queries. Compile SQL and templates before indexing; bind database catalog fields afterward. Missing template keys are errors.

Incrementally index modules below the project root by default, pin resulting immutable snapshots for all rules, and reuse one query pipeline. Dependencies supply context rather than lint targets. `--no-index` checks freshness without publication; `--snapshot` selects historical facts without refreshing. Partial or syntax coverage produces a fileless `uir/coverage` warning. Invalid queries, missing required metrics, indexing failures and template failures remain execution failures, preserving completed findings.

Expose a shared SQL row visitor without interactive pagination, retaining independent source witnesses even when SELECT omits path and line. Reuse the existing SQL evaluator, predicate, ordering and metric loading.

Reuse Gavel violation and linter result types, presentation, severity verdicts, baselines and snapshots. Add a native Gavel adapter that discovers `.uir.yaml` and executes `uir lint --format json --no-progress`. Preserve fileless diagnostics through file filtering. A config change reports the project; ordinary file scopes filter findings after full contextual analysis. Error findings and execution failures return nonzero; warnings and info alone succeed.

Implement shared behavior in Gavel first, then validate UIR against it locally. A released shared API must precede its pinned consumer; do not publish or commit local workspace overrides.

Verify with Ginkgo coverage for configuration/discovery/templates, uncapped rows, witnesses, SQLite/PostgreSQL, refresh/reuse/staleness/history/partial coverage, adapter protocol failures, config scope, severity and baselines. Add runnable examples and fixture-backed acceptance criteria, run focused tests plus build/lint gates, and report unrelated baseline failures separately. No autofixes, CEL rules, config inheritance, new UI or suppression changes.
