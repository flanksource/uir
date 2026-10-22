# Selected SQL Server migrations

This package converts complete database UIR snapshots into sqlglot migration inputs. It owns reference-to-target selection; SQL parsing, DDL generation, dependency ordering and destructive-change classification remain in sqlglot.

`Enrich(document, schema, scopes)` maps the complete native catalogue into UIR while retaining presentation metadata from the original inspection. `Schema(document)` projects it back and verifies coverage and definitions. Persist the UIR document with its `sqlCatalog` marker: an older, partial or unsupported document is rejected for migration planning.

`Select(Request{Reference, Target, Selections})` starts with the entire target and overlays selected reference definitions. Target-only selected members are removed; unselected target objects and members remain. `Plan` sends that desired state to the native SQL Server planner. `BuildArtifact` binds the same selection and explicit schema scope to a sqlglot artifact containing both snapshots, exact SQL and a deterministic SHA-256 hash.

Selections support whole tables, views, procedures, functions and triggers, plus individual columns, indexes, foreign keys and checks. Whole-table and whole-view selections include their triggers. Partial table selections require the parent on both sides. Unknown, duplicate or overlapping selections fail. Dependencies must remain valid after selection; the caller must explicitly select any required dependent changes.

Primary keys and UNIQUE constraints use their named index selection IDs. SQL module definitions must name their schema explicitly, matching the native planner's qualified-name contract.

Selection IDs are stable quoted identities, such as `table:"dbo"."orders"` and `column:"dbo"."orders"/"label"`. Use `Selection.ID()` and `ParseSelection` to create and validate them. The quoting preserves names containing dots, slashes and quotes.

Run the focused tests from a workspace containing the updated sqlglot module:

```sh
go test ./sql/migrate -count=1
go run ./examples/sql-migration
```

The example prints the artifact hash and a single column alteration while preserving a target-only column. It opens no database connection. Planning returns ordered SQL steps and destructive classifications. Sqlglot's `ValidateArtifact` and `CheckArtifact` validate integrity, baseline drift and desired-state idempotence. Commons-db's `migrate/sqlserver` executes a reviewed artifact in a transaction; the application owns authorization, trusted storage and execution records.
