# Publish original Git sources

An external publication can retain original source in Git while storing its extracted facts in UIR. `PublishedDocument.PathKey` remains the producer's logical document path. `PublishedDocument.GitPath` names the original file relative to the repository root. These paths can differ.

Set `ExternalLocation.RepositoryURI` and a resolved `PublishOptions.GitCommit` when any document has `GitPath`. Keep supplying the exact original bytes in `Source` and their SHA-256 in `ContentHash` during publication; UIR validates that pair before writing. A generated or transformed document leaves `GitPath` empty and uses the existing source blob store. One publication can contain both kinds.

```go
publication.Location.RepositoryURI = "/path/to/rules.git"
publication.Options = indexer.PublishOptions{
    Historical: true,
    GitCommit:  commitSHA,
    Producer:   producerFingerprint,
}
publication.Documents[0].GitPath = "rules/pc/Example/XMLData.xml"
result, err := indexer.Publish(ctx, database, publication)
```

The binding is part of source revision identity and publication hashes. Moving the original file creates a new binding even when its bytes and logical path are unchanged. Existing snapshots retain their original binding. A registered external location cannot change repositories; give a different repository a distinct location URI.

`Pipeline.ReadModuleSources` resolves the snapshot's bindings, retrieves original files at the pinned commit, and verifies every returned SHA-256. Local absolute paths and `file://` URIs accept working repositories and bare clones. Reads use NUL-delimited native Git batches, including paths containing tabs or newlines; Git 2.42 or newer is required. A missing object, failed reader, or mismatched hash is an error.

Applications with authenticated remote repositories provide their existing cache through `query.WithGitSourceReader`. The callback receives the repository URI, immutable commit, and repository-relative paths. It must return bytes in the same order. UIR checks result count and content hashes independently.

```go
pipeline, err := query.NewPipeline(database,
    query.WithGitSourceReader(readFromRepositoryCache),
)
// readFromRepositoryCache has type query.GitSourceReader:
// func(context.Context, query.GitSourceOptions) ([][]byte, error)
```

Historical publication continues to preserve location heads. Generated manifests can pin the returned module snapshot IDs using the existing source/blob and snapshot model; Git source bindings require no separate catalog database.

## Publish changed packages

An external historical publication can supply a delta against `Options.BaseSnapshotID`. Set `Delta` and provide every document of each changed package. Packages omitted entirely retain their existing document membership, source revisions and source metrics without loading their bytes. List deleted paths explicitly, including every removed file of a package that no longer exists.

```go
publication.Options.BaseSnapshotID = previous.SnapshotID
publication.Delta = &indexer.PublicationDelta{
    DeletedPaths: []string{"retired/XMLData.xml", "retired/metadata.json"},
}
publication.Documents = changedPackageDocuments
result, err := indexer.Publish(ctx, database, publication)
```

The base must have the same root, producer configuration, repository origin and indexed coverage. Partial packages, missing bases, unknown deletions and replacing and deleting the same path are errors before publication. An empty delta can retain all packages at a new commit. Without `Delta`, `Documents` remains a complete replacement. Historical heads stay unchanged in both cases.

Generated source reads use bounded SQL batches through `storage.ReadSourceBlobs`. Every stored hash is verified, and a missing or corrupt blob fails the read. Original Git sources continue through the configured Git reader in a native batch.

## Read selected source revisions

`storage.EffectiveSources` accepts `EffectiveSourcesOptions{SnapshotID, Paths, Prefix, Suffix}`. A nil `Paths` slice selects every effective source; an empty non-nil slice selects none. Prefix and suffix filters match literal paths, including percent and underscore characters. Filters apply while resolving source deltas, before loading revision records, so reading comparison metadata does not hydrate unrelated XML bindings. Every selection still validates the complete snapshot base chain and respects deletions.

`Pipeline.ReadModuleSources` passes the requested document paths through this projection. It reuses the verified revision records during Git retrieval rather than querying all source revisions again.

Run the focused checks:

```sh
go test ./indexer ./query -run 'Test(Indexer|Query)$' -ginkgo.focus='Git-backed external publication|historical publication options|external delta publication|reading many module sources' -ginkgo.no-color
go test ./storage -run TestStorage -ginkgo.focus='UirDB|source blobs' -ginkgo.no-color
pnpm --dir packages/api test test/publication.spec.ts
```
