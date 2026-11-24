# How UIR works

UIR gives code, SQL, and domain-specific data a shared vocabulary of symbols and relationships. The two diagrams show how producers publish those facts and how the CLI uses them.

## Everything lowers into one model

<ProductDiagram kind="inputs" caption="The Go indexer builds typed facts directly. SQL, XML, and other producers can publish validated JSON with their own symbol kinds and properties." />

See [module indexing](/module-indexing) and [historical imports](/historical-import) for the input and publication contracts.

## One model answers many questions

<ProductDiagram kind="outputs" caption="Queries, call graphs, symbol diffs, SQL lint rules, and the browser read the same model. Snapshots are stored in SQLite or PostgreSQL." />

Use the [getting started guide](/getting-started) to index a sample module and trace its calls.
