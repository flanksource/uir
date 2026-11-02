# CLI configuration

The following persistent options apply to UIR commands. Run `uir --help` or `uir <command> --help` for command-specific options and output settings.

| Option | Environment | Default and effect |
| --- | --- | --- |
| `--dsn <string>` | `UIR_DSN` | Database connection: a PostgreSQL DSN, `sqlite://` URL, or `.db` path. A nonempty flag takes precedence over the environment variable; if both are empty, UIR uses `~/.config/uir/uir.db` and creates its parent directory. Other SQLite paths need an existing parent directory. |
| `--schema <string>` | `UIR_SCHEMA` | PostgreSQL schema. A nonempty flag takes precedence over the environment variable; if both are empty, UIR uses `public`. A non-default schema requires PostgreSQL. |
| `--cpuprofile <path>` | — | Empty by default. Write a CPU profile for the command to the specified file. |
| `--memprofile <path>` | — | Empty by default. Write a heap profile after garbage collection when the command ends. |
| `--trace <path>` | — | Empty by default. Write a Go execution trace for the command. |

Before opening an existing database with a newer UIR version, back it up and consult the [storage cutover instructions](../storage/README.md). Some older layouts lose index snapshots or legacy project history when opened and require reindexing.

## Shared flags

These flags are inherited by commands. Boolean flags default to false unless stated otherwise; string values default to empty.

| Flags | Effect |
| --- | --- |
| `-i, --interactive` | Fill command arguments using an interactive form. |
| `-P, --properties <key=value>` | Set system properties; accepts a string-to-string map, empty by default. |
| `--format <spec>` | Select `pretty`, `json`, `ndjson`, `toon`, `yaml`, `csv`, `html`, `markdown`, `llm`, `pdf`, or `slack`. A bare format writes to stdout; `format=path` writes a file. Combine comma-separated sinks, for example `json=out.json,markdown=summary.md`. The default is pretty output. |
| `--pretty`, `--json`, `--yaml`, `--csv`, `--html`, `--markdown`, `--llm`, `--pdf` | Select the named output format. LLM output includes up to 25 rows with paging information. |
| `--table`, `--tree` | Add table or tree structure to the selected output format. |
| `--filter <CEL>` | Filter output data with a CEL expression. |
| `--dump-schema`, `--no-color` | Print the output schema to stderr for debugging, or disable colored output. |
| `--log-level <level>`, `-v, --loglevel` | Set the default log level (default `info`), or increase verbosity with a repeatable count. |
| `--json-logs`, `--log-to-stderr`, `--report-caller` | Use JSON logs, send logs to stderr (default true), or include caller information. |
| `--timings[=<path>]` | Report resource timings: a bare flag writes a Server-Timing line to stderr; a path writes JSON. |
| `--no-progress` | Disable task progress display. |
| `--max-concurrent <int>`, `--max-retries <int>` | Limit concurrent tasks (default 4; 0 means unlimited) and retries (default 3). |
| `--retry-delay <duration>`, `--graceful-timeout <duration>` | Set the base retry delay (default `1s`) and interrupt shutdown timeout (default `10s`). |
| `-h, --help`, `--version` | Show help or the version. |
