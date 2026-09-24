# Compact query grammar implementation plan

Status: implemented in the UIR query parser, snapshot-aware index evaluator, CLI/API envelope, browser query controls, tests, and query guide.

1. Replace the predicate grammar with Go qualified subjects, single-edge relations, filters, set operators, and bounded call paths.
2. Resolve subjects to active canonical symbols in each selected primary head, registered checkout, or historical snapshot. Return candidates for ambiguous spellings.
3. Read indexed postings for locations, project rows to canonical symbols for composition, and traverse indexed calls for bounded reachability. Include known interface dispatch on incoming edges.
4. Expose location rows, coverage, and a shortest witness path in the existing query envelope, then connect the browser examples and symbol actions to the new expressions.
5. Verify parser errors, each relation and filter, composition, graph bounds, CLI/API serialization, browser behavior, and repository gates.

Scope boundary: this language answers symbol navigation and recorded call reachability. General joins, dataflow, and sanitizer-aware taint remain outside it.
