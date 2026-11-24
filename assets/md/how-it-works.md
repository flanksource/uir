# How UIR works

UIR gives code, SQL, and domain-specific data a shared vocabulary of symbols and relationships. The diagram shows how producers lower and publish those facts.

## Everything lowers into one model

<ProductDiagram kind="inputs" caption="The Go indexer builds typed facts directly. SQL, XML, and other producers can publish validated JSON with their own symbol kinds and properties." />

See [module indexing](/module-indexing) and [historical imports](/historical-import) for the input and publication contracts.

Explore [what the model can answer](/#one-model-answers-many-questions), or use the [getting started guide](/getting-started) to index a sample module and trace its calls.
