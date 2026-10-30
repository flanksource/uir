package query

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

type GitSourceOptions struct {
	RepositoryURI string
	Commit        string
	Paths         []string
}

// GitSourceReader returns original bytes in requested path order.
type GitSourceReader func(context.Context, GitSourceOptions) ([][]byte, error)

func readExternalSources(ctx context.Context, database *gorm.DB, scope moduleScope, sources []verifiedSource, reader GitSourceReader) []sourceRead {
	reads := make([]sourceRead, len(sources))
	revisions := make(map[string]storage.SourceRevision, len(sources))
	var missing []string
	for _, source := range sources {
		if source.revision != nil {
			revisions[filepath.ToSlash(source.path)] = *source.revision
		} else {
			missing = append(missing, filepath.ToSlash(source.path))
		}
	}
	if len(missing) > 0 {
		loaded, err := storage.EffectiveSources(ctx, database, storage.EffectiveSourcesOptions{SnapshotID: scope.snapshot.ID, Paths: missing})
		if err != nil {
			for index := range reads {
				reads[index].err = err
			}
			return reads
		}
		for path, revision := range loaded {
			revisions[path] = revision
		}
	}
	var pending []int
	var paths []string
	var stored []int
	var hashes []string
	for index, source := range sources {
		revision, found := revisions[filepath.ToSlash(source.path)]
		switch {
		case !found || revision.ContentHash != source.hash:
			reads[index].err = fmt.Errorf("source %q does not match snapshot %s", source.path, source.snapshot)
		case revision.GitPath != "":
			pending, paths = append(pending, index), append(paths, revision.GitPath)
		default:
			stored, hashes = append(stored, index), append(hashes, source.hash)
		}
	}
	if len(stored) > 0 {
		contents, err := storage.ReadSourceBlobs(ctx, database, hashes)
		for _, index := range stored {
			if err != nil {
				reads[index].err = fmt.Errorf("read generated sources of external snapshot %s: %w", scope.snapshot.ID, err)
			} else {
				reads[index] = sourceRead{content: contents[sources[index].hash], origin: "snapshot"}
			}
		}
	}
	if len(pending) > 0 {
		readBoundGit(ctx, scope, sources, reads, pending, paths, reader)
	}
	return reads
}

func readBoundGit(ctx context.Context, scope moduleScope, sources []verifiedSource, reads []sourceRead, pending []int, paths []string, reader GitSourceReader) {
	var contents [][]byte
	var err error
	if scope.location.RepositoryURI == nil || *scope.location.RepositoryURI == "" || !pinnedGitRevision.MatchString(scope.snapshot.GitCommit) {
		err = fmt.Errorf("snapshot %s has an incomplete Git source binding", scope.snapshot.ID)
	} else {
		if reader == nil {
			reader = readLocalGitSources
		}
		contents, err = reader(ctx, GitSourceOptions{RepositoryURI: *scope.location.RepositoryURI, Commit: scope.snapshot.GitCommit, Paths: paths})
		if err == nil && len(contents) != len(pending) {
			err = fmt.Errorf("git source reader returned %d results for %d paths", len(contents), len(pending))
		}
	}
	for position, index := range pending {
		source := sources[index]
		switch {
		case err != nil:
			reads[index].err = fmt.Errorf("read Git source %q of snapshot %s: %w", source.path, source.snapshot, err)
		case contentHash(contents[position]) != source.hash:
			reads[index].err = fmt.Errorf("git source %q at %s does not match indexed content hash", source.path, scope.snapshot.GitCommit)
		default:
			reads[index] = sourceRead{content: contents[position], origin: "git"}
		}
	}
}

func readLocalGitSources(ctx context.Context, options GitSourceOptions) ([][]byte, error) {
	directory := options.RepositoryURI
	if parsed, err := url.Parse(directory); err == nil && parsed.Scheme == "file" && parsed.Host == "" {
		directory = parsed.Path
	}
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("git repository %q requires a configured Git source reader", options.RepositoryURI)
	}
	objects := make([]string, len(options.Paths))
	for index, path := range options.Paths {
		if !fs.ValidPath(path) || path == "." || strings.ContainsRune(path, 0) {
			return nil, fmt.Errorf("invalid Git source path %q", path)
		}
		objects[index] = options.Commit + ":" + path
	}
	output, err := catFileBatch(ctx, directory, objects)
	if err != nil {
		return nil, err
	}
	contents := make([][]byte, len(objects))
	for index, object := range objects {
		contents[index], err = nextGitBlob(&output, object)
		if err != nil {
			return nil, err
		}
	}
	if len(output) != 0 {
		return nil, fmt.Errorf("git source reader returned %d extra bytes", len(output))
	}
	return contents, nil
}
