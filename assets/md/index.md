<section className="uir-hero" aria-labelledby="uir-title">
  <h1 id="uir-title">One model for every codebase you run.</h1>
  <p>UIR is the Universal Intermediate Representation. It lowers Go, SQL routines, XML, and domain-specific sources into one model of code. Index a repository once, then query symbols, trace calls, enforce SQL rules, and diff changes across commits.</p>

  <div className="uir-actions">
    <Link className="uir-action-primary" to="/getting-started">Get started</Link>
    <Link to="/how-it-works">How it works</Link>
    <a href="https://github.com/flanksource/uir">View on GitHub</a>
  </div>
</section>

## What UIR does

<div className="uir-features">
  <Link to="/module-indexing" className="uir-feature">
    <h3>Many sources, one model</h3>
    <p>Lower Go, PL/pgSQL, T-SQL, and XML, or publish symbols and relationships from your own domain.</p>
  </Link>

  <Link to="/storage" className="uir-feature">
    <h3>Snapshot indexing</h3>
    <p>Build immutable snapshots in SQLite or PostgreSQL with <code>uir add</code> and <code>uir reindex</code>.</p>
  </Link>

  <Link to="/query" className="uir-feature">
    <h3>Query with selectors or SQL</h3>
    <p>Find symbols and follow relationships, or use SQL to query metrics and aggregate results.</p>
  </Link>

  <Link to="/graph" className="uir-feature">
    <h3>Bounded call graphs</h3>
    <p>Explore callers and callees to a set depth and see the conditions around each call.</p>
  </Link>

  <Link to="/lint" className="uir-feature">
    <h3>SQL lint rules</h3>
    <p>Enforce architecture rules in CI and report violations at the source line.</p>
  </Link>

  <Link to="/diff" className="uir-feature">
    <h3>Rename-aware diffs</h3>
    <p>Compare symbols across Git commits and see changes to signatures, structure, and bodies.</p>
  </Link>
</div>

## Index and query a Go module

After [installing the CLI](/getting-started#install-the-cli), run `uir add` from a Go module, then find a function and its callers. Replace `Run` with the function you want to trace.

```bash
uir add .
uir query 'func:Run <'
```

[Read the docs introduction](/overview) or use the [Go model](/model) directly.
