# Historical external snapshots

External producers can publish a resolved Git commit without changing the current location head. Create a publication JSON using the [publication format and examples](module-indexing.md#publishing-from-a-non-go-producer). Set `PUBLICATION` to its path, `COMMIT` to the full ID resolved in the source repository with `git rev-parse HEAD^{commit}`, and `PRODUCER` to a fingerprint identifying the producer executable and effective lowering configuration. Supply `options.historical`, `options.git_commit` and `options.producer` in the publication JSON, or pass those values as flags:

```bash
mkdir -p .tmp
uir --dsn .tmp/history.db import "$PUBLICATION" --historical --git-commit "$COMMIT" --producer "$PRODUCER"
```

`git_commit` is a full lowercase SHA-1 or SHA-256 Git object ID. `producer` identifies the executable and effective lowering configuration. Missing historical identity, unresolved commit names and conflicting flag/publication metadata are rejected. The zero-value options publish a current head.

Matching historical requests reuse a retained snapshot when commit, revision, content, producer configuration, context, dependencies and coverage match. A changed producer fingerprint invalidates reuse. Publication records a delta from the current head when one exists while preserving that head and its version. A location created only through historical publication has no current head: location listings omit head metadata, and the snapshot browser and explicit snapshot queries can read its retained history.

Producer sources can combine or transform Git files. External source bytes therefore remain in UIR's source blob store; their commit identity does not imply that a corresponding source file can be reconstructed directly from Git.

The OIPA offline producer resolves and archives committed rules, lowers them through its existing filesystem VFS, and atomically publishes modules plus a complete catalog manifest:

```bash
oipa-cli symbols index --dir /path/to/rules-repository --revision HEAD --dsn .tmp/rules.db
oipa-cli symbols index --dir /path/to/rules-repository --revision HEAD~1 --historical --dsn .tmp/rules.db
```

It fingerprints its executable and effective OIPA configuration by default; `--producer` supplies a producer identity that is still combined with the configuration. `--memprofile` writes a heap profile; global CPU and execution-trace flags support profiling. The command runs offline without initializing the live environment database. Its generated HTTP action requires administrator permission because it reads server files and writes a caller-selected output database.

Sources and embedded source locations are relative to the archived repository root, including when `--dir` selects a subtree. Temporary archive directory names do not participate in source or document identity, so unchanged committed sources can reuse their stored revisions across archive extractions.

Complete historical manifests are reused before lowering. New current catalogs retire heads for roots they no longer contain. All catalog changes and automatic pruning share one transaction; a failed manifest write rolls back every module. Automatic pruning keeps historical snapshots, while explicit pruning can remove them according to the requested retention count.

For the real-corpus Gavel replay, see the [snapshot benchmark runner](../hack/snapshotbench/README.md).
