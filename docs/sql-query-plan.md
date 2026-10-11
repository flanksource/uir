# SQL navigation across code and custom domains

Status: implemented with Participle v2 and verified on 2026-10-10. See [SQL navigation](sql-query.md) for usage and [the acceptance fixture](../fixtures/sql-query.md) for validation.

## Summary

Add SQL alongside compact queries, with automatic detection, typed symbol selectors, domain functions, and native relationship traversal.

Custom domains register **symbol kinds, typed properties, and relationship kinds**. The grammar and graph engine consume that catalog; concrete HTTP, messaging, and Kubernetes extractors remain separate work.

## Grammar and matching

```sql
SELECT symbol, path, line
FROM callers(method:store.Store.Save)
WHERE symbol.name IMATCH '*save*';

SELECT *
FROM http.route:* AS r
WHERE r.method = 'POST'
  AND r.path IMATCH '/orders/**';

SELECT *
FROM incoming(messaging.topic:OrderCreated,
              via => 'messaging.publishes_to');

SELECT *
FROM graph(k8s.service:checkout,
           via => 'k8s.selects,k8s.uses_config',
           direction => 'outgoing', depth => 3);
```

```ebnf
query      = SELECT projection FROM source [AS identifier]
             [WHERE predicate] [ORDER BY ordering] [LIMIT integer] [";"];

projection = "*" | field [AS identifier]
             {"," field [AS identifier]};

source     = selector | invocation;
invocation = identifier "(" source {"," source}
             {"," identifier "=>" literal} ")";
selector   = prefix ":" pattern [":" pattern];

predicate  = comparison | membership | glob_match | null_check
             | NOT predicate | "(" predicate ")"
             | predicate AND predicate | predicate OR predicate;

comparison = field ("=" | "!=" | "<" | "<=" | ">" | ">=") literal;
membership = symbol [NOT] IN selector;
glob_match = field [NOT] (MATCH | IMATCH) string;
null_check = field IS [NOT] NULL;
ordering   = field [ASC | DESC] {"," field [ASC | DESC]};
```

- `MATCH` performs case-sensitive glob matching; `IMATCH` uses Unicode simple case folding. Both retain existing anchored glob semantics: `*`, `?`, slash-delimited `**`, escaping, and `/...`.
- Matching never changes canonical identity or stored names. Typed selector patterns remain case sensitive; use `IMATCH` in `WHERE` for insensitive filtering.
- Keywords are case insensitive. Single quotes hold ordinary strings; double quotes delimit selector values needing spaces or punctuation.
- Preserve `pkg:`, `mod:`, `method:`, `func:`, `field:`, `var:`, `type:`, and `kind:` semantics. `func:` continues to select registered callable kinds.
- Every registered kind can supply a prefix, including `table:`, `column:`, and namespaced custom kinds. `api:` aliases the existing `endpoint` kind; `http.route:` is a distinct custom kind when registered.
- Version one supports one source, projection, predicates, ordering, and limits. Joins, aggregation, subqueries, CTEs, and database writes are excluded.

## Custom-domain contract

Each producer declares its kinds and relationships, then publishes snapshot-bound facts through the existing external-publication pipeline.

| Domain | Entities | Directed relationships |
|---|---|---|
| HTTP | Routes, clients, handlers | Client → route: `http.requests`; route → handler: `http.handled_by` |
| Messaging | Topics, queues, producers, consumers | Producer → topic: `messaging.publishes_to`; topic → queue: `messaging.routes_to`; queue → consumer: `messaging.delivers_to` |
| Kubernetes | Ingresses, services, workloads, pods, configuration | Ingress → service: `k8s.routes_to`; service → pod: `k8s.selects`; owner → child: `k8s.owns`; workload/pod → configuration: `k8s.uses_config` |

### Identity and properties

- Preserve canonical identity construction. Producers include distinguishing context through module, package, owner, and name: service context for HTTP; broker/virtual-host context for messaging; cluster and namespace for Kubernetes.
- Kind declarations expose a JSON Schema for queryable properties. Publish property values on snapshot-specific declarations, accessible through `symbol.props`, while preserving the complete producer payload.
- Examples include HTTP method/path, messaging protocol/topic configuration, and Kubernetes resource kind/labels.
- Validate properties during publication and bind property references against the selected kinds’ schemas. Unknown fields fail; absent optional fields are `NULL`.
- Keep kind allocation within the existing handle layout. Registry exhaustion remains an explicit error; expanding that layout is separate work.

### Relationships and evidence

- Register namespaced relationship kinds with a coarse UIR relationship type and optional property schema. Preserve the native name even when several relationships share the same coarse type.
- Store native relationships in a new version of indexed document content: source identity, target identities/reference, kind, properties, resolution status, and source evidence.
- Add source and target posting roles to the existing document-posting index. Relationship details remain in immutable documents selected through the existing snapshot mechanism.
- Preserve static, dynamic, ambiguous, and unresolved resolution. Candidates and unresolved references remain visible; name similarity does not create cross-domain links.
- Producers publish explicit bridges between domains. For example, HTTP route → Go handler → messaging topic → queue → consumer. Traversal reports recorded possible reachability, not guaranteed execution.

## Navigation, execution, and interfaces

| Function | Result |
|---|---|
| `definitions(s)` | Declaration locations |
| `callers(s)`, `callees(s)` | Call-site rows |
| `references(s)`, `reads(s)`, `writes(s)` | Recorded use locations |
| `methods(s)`, `implementers(s)`, `inheritors(s)` | Existing declaration witnesses |
| `incoming(s, via => ...)`, `outgoing(s, via => ...)` | Native relationship witnesses |
| `traverse(s, direction => ..., via => ..., depth => ...)` | Reachable symbols with minimum hop distances |
| `path(a, b, via => ..., depth => ...)` | One shortest recorded path |
| `graph(s, direction => ..., via => ..., depth => ...)` | Nodes, edges, sites, properties, and omissions |

- Nested navigation functions consume the inner result’s canonical symbol projection. Graphs and paths are terminal results.
- `via` selects exact registered relationship names, supplied as a comma-separated string. Unknown names fail. Without `via`, traversal and paths follow calls and known dispatch.
- SQL directions are `incoming`, `outgoing`, and graph-only `both`. `both` unions incoming and outgoing walks without switching direction at intermediate nodes.
- Native traversal can start from any registered entity kind. Call-specific functions retain callable validation.
- Defaults: one hop for callers/callees, three for traversal, eight for paths; graph depth two and 150 nodes. Preserve depth 1–8, the traversal failure bound of 10,000 symbols, and graph maximum of 1,000 nodes.
- Retain existing graph exclusions and `access` behavior when using the existing call/data graph interface. `via` selects native relationships explicitly and cannot be combined with `access`.
- Include relationship kind in graph edge identity, grouping, ordering, and frontend merging so distinct relationships between the same endpoints survive.

### SQL results and integration

- `symbol` denotes the projected navigation result. `symbol IN selector` compares canonical identity in the selected snapshots.
- Expose symbol properties plus witness fields such as path, line, package, module, role, dispatch, depth, and snapshot.
- Apply `WHERE` after navigation, then ordering and limits. Filtering does not prune intermediate traversal nodes.
- Graph/path queries accept `SELECT *` without row clauses; function arguments control their bounds. Row queries default to 100 results, maximum 1,000, and retain pre-limit totals.
- Add `language: auto | compact | sql`. Automatically route a leading unquoted `SELECT` token to SQL; provide an explicit override. SQL failures never fall back to compact parsing.
- Keep root/location/snapshot request options. Structured navigation flags remain compact-only.
- Extend the existing result envelope with optional projected columns/rows and graph data; retain matches, paths, coverage, warnings, and stages.
- Reuse the query pipeline and graph builder. Add the SQL parser and registry-backed validation/completion, then wire CLI, API, Query page, and Cmd+K together.
- Older snapshots remain usable for their existing facts. Requests for native relationships absent from their document format report missing capability and reindex/reimport guidance.

## Validation and delivery

- Add failing Ginkgo tests for grammar, detection, function binding, typed selectors, nested navigation, and `MATCH`/`IMATCH`, including Unicode and escaped globs.
- Test property schemas, optional values, identity collisions across contexts, publication round trips, relationship postings, and historical capability reporting.
- Use fixtures for an HTTP → handler → topic → consumer path and Kubernetes selection/configuration relationships, including parallel relationship kinds between the same nodes.
- Verify cycles, dispatch, ambiguous/unresolved targets, both traversal directions, shortest paths, bounds, and omission metadata.
- Extend API integration and focused Vitest coverage. Verify completion, diagnostics, source navigation, and graphs live with agent-browser.
- Run the tracked TODO fixture, `go test ./...`, `make lint`, and `make build`.
