# Selected SQL Server migrations

This package converts database UIR catalogues into sqlglot migration inputs and plans the migration of one catalogue toward selected parts of another. It owns reference-to-target selection; SQL parsing, diffing, DDL generation, ordering, forced rebuilds of dependent objects and destructive-change classification belong to sqlglot's `migrate/engine`, which this package drives through its registered `sqlserver` dialect.

`Enrich(document, schema, scope)` maps a catalogue into UIR while retaining presentation metadata from the original inspection. `Schema(document)` projects it back and verifies coverage and definitions. Persist the UIR document with its `sqlCatalog` marker: an older or unsupported document is rejected for migration planning. Table types and sequences have no UIR node of their own and live on the catalogue as `sqlCatalog.tableTypes` and `sqlCatalog.sequences`, so a routine taking a table-typed parameter stays plannable. Retained metadata covers source locations, descriptions, annotations and row counts of tables and their columns; every fact the catalogue holds comes from the catalogue. Table types and sequences are selected whole as `table-type:"schema"."name"` and `sequence:"schema"."name"`. A selected routine taking a table type the desired schema lacks fails, naming the `table-type` selection to add: the type is never pulled in implicitly.

## Complete and declared catalogues

`scope.Mode` of `Enrich` says what the catalogue claims, and is recorded as `sqlCatalog.complete` or `sqlCatalog.declared`. The two are explicit and exclusive: `Schema` refuses a catalogue that is neither (a partial inspection, or one stored before the marker existed with `complete: false`) with the same error as a missing catalogue, and one that is both.

| Mode | Meaning | Produced by |
| --- | --- | --- |
| `engine.ScopeComplete` | The catalogue holds every object of `scope.Schemas`. An object it lacks does not exist. | An inspection of a database. |
| `engine.ScopeDeclared` | The catalogue holds only the objects it names. It says nothing about any other object. A declared table is complete unless it is marked partial. | An HCL declaration. |

The target of a request is the baseline the plan applies to, so it must be complete. The reference may be either, and its mode selects the scope of the plan:

- A complete reference plans in complete scope over the schemas of the catalogues, which must be the same. Selecting a member only the target holds removes it from the desired schema, so the plan drops it and the drop is destructive and acknowledged.
- A declared reference plans in declared scope with `Scope.Objects` set to the objects it declares. Its schemas must be among the target's. A declared table is complete unless its declaration is marked partial (`partial = true` in HCL, `RecordTable.Partial` in the catalogue, `Table.Partial` in the planner schema). Target objects the reference does not declare are left out of the changes and of `Preserved`, and are never dropped; the artifact lists them as `Context`, because the dependents of a declared column can sit on them and the baseline must hold those.

A complete declared table states every member the table should have. The columns, indexes, foreign keys and checks the target holds and the declaration omits can be selected (`column:"dbo"."orders"/"total"`, or the whole table), and each selected one is planned as a destructive drop that must be acknowledged. Nothing is dropped that is not selected.

A partial declared table names only the members it wants to manage. What the target holds and the declaration omits is kept: selecting the whole table lists those drops under `Preserved` and plans none of them, and selecting one of them is refused, naming the table as partial. A partial table the target does not have fails the plan, because a partial declaration cannot create a table. A catalogue read from a database is never partial: `Enrich` and `Schema` refuse a partial table in a complete catalogue.

## Planning

`Select(Request{Reference, Target, Selections})` starts with the entire target and overlays selected reference definitions. Selected members only the target holds are removed from the desired schema, for a complete reference and for a complete declared table; unselected target objects and members remain. It returns the `engine.PlanRequest` for that difference, after planning it once to prove the selected desired schema is valid. `BuildArtifact` plans the same request and returns the `engine.Artifact` (version 2): the changes with their IDs and `requires`, the rebuilds the changes force (an index, key, foreign key, check or default on a retyped column is dropped and created again, each recorded as a `rebuild` change naming its cause), the ordered units of SQL with their preconditions, both snapshots and a deterministic SHA-256 hash. Planning is offline: no database is read, so the dependents of a changed column come from the target catalogue, and a dependent the target does not hold is not seen. `engine.Validate` checks integrity and re-plans the artifact from its own inputs; `engine.Check` and `engine.Apply` run it against a database, and `sqlserver.RenderScript` renders it as a reviewed T-SQL script.

Selections support whole tables, views, procedures, functions and triggers, plus individual columns, indexes, foreign keys and checks. Whole-table and whole-view selections include their triggers. Partial table selections require the parent on both sides. Unknown, duplicate or overlapping selections fail. Dependencies must remain valid after selection; the caller must explicitly select any required dependent changes.

Primary keys and UNIQUE constraints use their named index selection IDs. SQL module definitions must name their schema explicitly, matching the native planner's qualified-name contract.

Selection IDs are stable quoted identities, such as `table:"dbo"."orders"` and `column:"dbo"."orders"/"label"`. Use `Selection.ID()` and `ParseSelection` to create and validate them. The quoting preserves names containing dots, slashes and quotes.

Run the focused tests from a workspace containing the updated sqlglot module:

```sh
gavel test ./sql/migrate
go run ./examples/sql-migration
```

The example prints the artifact hash and a single column alteration while preserving a target-only column. It opens no database connection. The application owns authorization, trusted storage and execution records.
