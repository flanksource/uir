# SQL architecture linting

The [request-state example](../lint/testdata/request-state/.uir.yaml) rejects package-level variables of the exact request-scoped context, configuration, runtime and database types used by OIPA's Ruleguard rule. It uses `symbol.type`, including inferred types and aliases, and preserves each variable name's source location. Run `uir lint --config lint/testdata/request-state/.uir.yaml` with an isolated database to inspect the example. The example is a self-contained module; use `GOWORK=off` when running it from this development workspace. Incomplete typed coverage or missing type facts fail this rule's query rather than silently passing it.

`uir lint` executes rules from `.uir.yaml`. Each SQL row becomes a Gavel violation with its original source location, even when the SELECT list only contains metric values. Queries can combine metrics with the existing definitions, callers, callees, reads, writes and implementation navigation functions.

Copy [the runnable example](../lint/testdata/project/.uir.yaml) to your project root as `.uir.yaml`. It flags files with fewer than 100 lines and three definitions, functions with more than three parameters, and bodies longer than 50 lines.

```sh
uir lint
uir lint --all
uir lint --rule too-many-parameters --format json --no-progress
uir lint --config path/to/.uir.yaml --no-index
uir lint --config path/to/.uir.yaml --snapshot SNAPSHOT_ID
uir lint --include 'cmd/**' --exclude '**/*_test.go'
uir lint --exclude function-parameters
uir lint --exclude function-parameters --group-by symbol
uir lint --group-by file --format json
uir lint --changed
uir lint --changed HEAD~3..HEAD
uir lint --changed main...HEAD --group-by symbol
uir lint --staged
uir lint --tags integration,feature
```

`--tags` accepts repeated or comma-separated Go build tags. Tags select files for syntax analysis and typed loading and participate in analysis freshness checks; changing tags requires fresh analysis before `--no-index` can reuse it. Explicit snapshots already fix their build configuration and reject `--tags`. When running through Gavel, `gavel lint uir --generated=false --tags integration,feature` also suppresses generated-file findings while retaining generated source for type resolution.

Configuration discovery merges home configuration first, then configuration from the filesystem root down to the current directory, including ancestors above the Git root. Each directory loads `.uir.yaml` followed by the `.yaml` and `.yml` files directly inside `.uir.d/` in filename order. Home configuration includes both `~/.uir.yaml` and `~/.uir.d/` and is loaded once. Hidden files and other extensions inside `.uir.d/` are ignored. Each fragment uses the same complete version 1 document shape as `.uir.yaml`. Rules merge by `id`: a later file replaces the complete rule definition, while other inherited rules remain in their original order. `lint.include_tests` inherits when omitted; an explicit `true` or `false` overrides it. Every unexpired discovered file is validated, including overridden rules. `--config` selects one explicit file without inheritance.

An optional top-level `_expires` accepts an RFC3339 timestamp, for example `_expires: "2026-10-22T12:00:00Z"`. During configuration loading, UIR deletes a file whose timestamp has passed and does not validate or merge its rules. Future-dated files load normally and remain on disk; files without `_expires` are persistent. Invalid timestamps and deletion failures stop the run with the file path. Expiration is checked before analysis, not continuously during execution. If every configuration has expired, lint reports that no unexpired configuration exists.

The nearest configuration inside the repository defines the project root for file filters. A fragment's project root is the directory containing `.uir.d/`, not `.uir.d/` itself. With only home or external ancestor configuration, lint uses the Git root, or the current directory outside Git. Outside Git, a non-home ancestor configuration defines the project root. An explicit file defines its own project root. Discovery follows ancestors of the invocation directory; it does not recursively load configurations below it.

Analysis selects the enclosing Go module at the invocation directory, or at the explicit configuration's directory with `--config`. Running in a package subdirectory still analyzes its whole enclosing module. Nested modules are separate and require explicit selection. A directory without an enclosing `go.mod` fails with an actionable error.

`--all` selects every registered module's primary location, applies the selected configuration to all of them, and pins their snapshots into one combined query scope. Register modules with `uir add` first. Cross-module callers, callees and other navigation can then traverse those selected modules. External producer locations use their existing snapshots. Under `--all`, include/exclude file patterns are relative to each module. `--all` cannot combine with `--changed`, `--staged` or `--snapshot`; it supports `--no-index`.

Version 1 accepts `version`, optional `_expires`, `lint.include_tests` (default false) and `lint.rules`. Every unexpired file requires at least one rule with a unique `id`, SQL `query` and nonempty `message`. Severity is `error` by default, or `warning` or `info`. The IDs `uir/coverage` and `uir/index` are reserved for index warnings. Unknown fields, duplicate YAML keys, extra documents, invalid queries and invalid templates fail validation.

For shared rules, save this as `~/.uir.yaml`:

```yaml
version: 1
lint:
  include_tests: true
  rules:
    - id: function-parameters
      query: SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 3
      message: '{{.name}} has too many parameters'
      severity: warning
```

Save a project-specific replacement as `.uir.yaml` in the project root, then run `uir lint` there:

```yaml
version: 1
lint:
  include_tests: false
  rules:
    - id: function-parameters
      query: SELECT symbol.name AS name FROM func:* WHERE symbol.metrics.param_count > 4
      message: '{{.name}} exceeds the project parameter limit'
      severity: error
```

The project now checks its four-parameter limit without test files. Other home rules still run. Put another `.uir.yaml` in a subdirectory and run `uir lint` from that directory to apply its overrides to the enclosing module. Use file filters to restrict findings to a subtree. Use `uir lint --config .uir.yaml` to run only the project file.

`--exclude` disables exact configured rule IDs before execution, including rules selected with `--rule`. Other exclusion values and all `--include` values filter reported file paths relative to the project root, or each module with `--all`. Repeat either flag or comma-separate values; rule IDs and file globs can be mixed, for example `--exclude 'function-parameters,**/*_test.go'`. Exact rule IDs take precedence over file paths with the same name. Includes are alternatives; exclusions take precedence. File patterns use the SQL `MATCH` syntax: `*` stays within a path segment, `?` matches one character, `**` matches zero or more segments, and `/...` abbreviates `/**`. Quote globs to prevent shell expansion. Invalid globs fail before opening the database. File filters apply after querying the selected module scope, so excluding a callee's file does not hide a violation in an included caller. Fileless coverage warnings are always retained when rules run. Excluding every selected rule succeeds with zero rules and skips analysis and queries.

The repository's [`.uir.example`](../.uir.example) also provides guideline-based warnings, including command files over its configured size threshold and Cobra references outside any `cmd/` path segment. The command size threshold is a configurable heuristic for moving business logic into backend packages. Cobra detection uses canonical target package identities, so import aliases do not bypass it; it requires typed reference facts.

The example also includes `hack-build-tag`, a warning for indexed `.go` files under root or nested `hack/` directories whose modern build expression does not require `hack`. It accepts compound expressions such as `hack && linux`, but flags missing headers, legacy-only headers, `ignore`, `!hack`, and `hack || linux`. The query explicitly includes build-excluded files, so an unsuitable constraint or filename suffix cannot hide the file. Paths inside SQL remain module-relative. This rule reads snapshot-verified source headers and reports an execution error if the indexed bytes are unavailable; it never adds tags automatically. See [build-constraint queries](sql-query.md#build-constraints) for semantics and cold-query cost.

Run the self-contained [hack build-tag example](../lint/testdata/hack-build-tags/.uir.yaml) from this repository root:

```sh
make binary
GOWORK=off .bin/uir --dsn .tmp/hack-build-tag-example.db lint --config lint/testdata/hack-build-tags/.uir.yaml --rule hack-build-tag --format json --no-progress
```

The fixture deliberately includes violations and reports eight warnings, including an excluded platform file, a test file and a nested hack directory. `myhack/` and files that require `hack` do not produce findings. Warning-only runs exit zero. Use `gavel fixtures fixtures/hack-build-tag.md` for executable acceptance.

Messages use Go `text/template` with SELECT aliases as map keys. For example, `SELECT symbol.name AS name` exposes `{{.name}}`. Use `{{index . "symbol.name"}}` for an unaliased dotted column. Missing keys accessed by name fail execution. Rules return every matching row: explicit `LIMIT` and terminal `graph()`/`path()` results are rejected. The interactive query limit does not apply to lint.

Lint infers analysis requirements from the selected SQL rules. File metrics, build headers and direct callable metric queries use syntax analysis without loading Go packages or resolving imported modules. Type fields, canonical identities, references, relationships and navigation functions use typed analysis. Typed analysis loads imported type information to resolve the current module's facts; it publishes documents only for the selected module and never recursively indexes dependencies. Normal `uir add` and `uir reindex` retain their existing dependency indexing behavior.

Syntax and typed snapshots have separate persisted analysis heads and do not advance normal index heads. Repeated runs reuse unchanged analysis, including dirty checkouts, without loading packages or publishing another snapshot. Reuse validates source bytes, manifests, build options, test selection and the indexer version. Typed reuse also validates imported local package source and workspace module manifests; changes in unrelated workspace package source do not invalidate it.

`--no-index` requires fresh analysis of each needed mode and fails explicitly when it is missing or stale. It never publishes snapshots. `--snapshot` selects historical indexed facts and skips freshness checks; combine it with the configuration that should evaluate that history. The two flags are mutually exclusive. Mixed configurations execute syntax rules before typed rules, then restore configuration order in the report. A typed-analysis failure preserves completed syntax findings.

`--changed` reports tracked differences from HEAD plus nonignored untracked paths while analyzing the live checkout. An optional commit or range selects an immutable source: a single commit compares with its first parent (or the empty tree for an initial commit), `A..B` compares endpoints and analyzes B, and `A...B` compares the unique merge base with B and analyzes B. Omitted range endpoints mean HEAD. `--staged` analyzes the exact captured index, including its staged `.uir.yaml` and module manifests; later staged or unstaged edits cannot alter that capture. Conflicted indexes fail explicitly. Both modes require a local Git checkout.

Changed paths intersect the normal include/exclude filters. Analysis retains the full module context, findings retain original checkout paths, and grouping and the exit verdict use only the filtered findings. Deletions remain part of the selection and renames are deletion/addition pairs. A selection with no reportable paths succeeds without opening the database or indexing; it never becomes an unrestricted run. Fileless coverage warnings remain when a selected run executes. A manifest or configuration change selects that path; it does not automatically widen the selection to every Go file.

Git source preparation belongs to `commons-db/git`. Bare `--changed` requires Git 2.45 or newer to disable lazy object fetching explicitly. Pinned source trees are materialized under project `.tmp/`, with raw Git blob bytes and no checkout filters or LFS hydration. Unsafe symlinks and gitlinks fail explicitly. Commit and staged snapshots publish historically under original checkout locations and do not advance saved checkout heads. Staged snapshots use the captured tree ID as their revision and store their source bytes. Pinned extraction follows historical indexing's isolated Go configuration: `GOWORK=off` and local replacements removed from a temporary module manifest, so it cannot borrow mutable external checkout replacements. `--config` names a path in the selected repository; pinned modes load its captured contents rather than the current file. Without `--config`, discovery merges captured ancestor configurations up to the selected source root, excluding home and mutable external ancestors. Bare `--changed` uses normal home and ancestor inheritance from the live checkout.

`--changed` and `--staged` are mutually exclusive. Neither combines with `--snapshot`. Bare `--changed` supports `--no-index`; commit/range and staged sources require indexing and reject `--no-index`. A positional revision without `--changed` fails before database access. The shared Gavel linter runner provides execution timeout, metadata, violation filtering and failure verdicts directly in-process; this command does not invoke `gavel lint uir`.

Every diagnostic of a package that is not fully indexed is a warning on its file (fileless when the toolchain gave no file), grouped with that file's findings and scoped by `--changed` and `--staged` like them: `uir/coverage` for a load or type error, `uir/index` for a file whose own document failed and was indexed degraded (from its syntax only, or as an empty document) so the rest of the module still indexes; rules continue against available facts. Intentional syntax analysis has no load diagnostics, so it reports only `uir/index` warnings. A typed rule against an explicit syntax snapshot fails rather than silently returning no rows. Missing required metrics, catalog binding errors, analysis failures and message failures are execution errors. Findings completed before an execution failure remain in the JSON report. File-level findings have no invented line or column.

`--group-by symbol|file` groups findings for review. File grouping is the default display. Symbol grouping combines findings from different rules under the declaration owning the source location; call and relationship findings belong to the caller/source symbol, independently of the SQL projection. Groups use module, file and declaration span, so syntax and typed findings for the same declaration combine while identically named declarations stay distinct. Syntax findings carry a declaration witness without an invented canonical symbol ID. Findings without a symbol appear under file findings, and fileless coverage warnings appear under run diagnostics. Grouping preserves every finding, its severity and location, and the command's exit status. Explicit grouping also adds `group_by` and a `groups` array to JSON when findings exist; each group contains `file`, optional `declaration_key`, `symbol_id` and `symbol`, and `violations`. The flat `violations` array remains available for Gavel consumers. Omitting the flag preserves the standard report JSON.

Error findings and execution failures exit nonzero. Warnings and info alone exit zero. JSON output is an array containing the Gavel `LinterResult` fields, including `success`, `error`, `duration`, `violations`, `file_count` and `rule_count`. `success` describes execution, so an error-severity finding may appear in a successful report while the command exits nonzero.

Gavel discovers global and project UIR configuration and runs the installed `uir` binary as a native linter. Use `gavel lint uir` for Gavel snapshots, baselines, summaries and UI. Native documents configured through `lint.linters.uir.config` in `.gavel.yaml` are written into a unique project `.uir.d/gavel-*.yaml` fragment with a deadline-based `_expires`, then removed when execution finishes. UIR performs configuration validation and merging; Gavel does not import the UIR library. Analysis runs with the selected module context; a normal file scope filters reported findings afterward. Changing `.uir.yaml` or a `.uir.d/` fragment reports the full owning module. Fileless run warnings survive file scopes.

The UIR dependency is pinned to Gavel v0.0.56 for existing report types. The shared `linters.HasFailures` API and native adapter are implemented in the sibling Gavel checkout and currently validated through the ignored local workspace. Release Gavel containing those changes, then update the UIR pin and run `GOWORK=off make build` before publishing UIR. No local replacement belongs in go.mod.

The changed/staged modes also consume `commons-db/git`'s prepared-source API, currently implemented in the sibling checkout. The commons-db v0.1.61 pin does not provide it. Publish those shared Git changes and update the commons-db pin before validating or publishing a standalone UIR release.

To run the included sample against an isolated database:

```sh
make binary
GOWORK=off .bin/uir --dsn .tmp/lint-example.db lint --config lint/testdata/project/.uir.yaml --format json --no-progress
```

The sample reports one warning and one parameter-count error. `GOWORK=off` selects the sample's own module rather than the repository's development workspace. To see a warning-only successful run, add `--rule small-files`.
