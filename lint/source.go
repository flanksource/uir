package lint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/commons-db/git"
	"gorm.io/gorm"
)

type Request struct {
	Config  *Config
	Options Options
}

func (options Options) Validate() error {
	switch {
	case options.Changed && options.Staged:
		return errors.New("--changed and --staged cannot be combined")
	case options.Revision != "" && !options.Changed:
		return errors.New("a revision requires --changed")
	case options.Snapshot != "" && (options.Changed || options.Staged || options.NoIndex):
		return errors.New("--snapshot cannot be combined with --changed, --staged or --no-index")
	case options.NoIndex && (options.Staged || options.Revision != ""):
		return errors.New("--no-index cannot be combined with a commit range or --staged")
	}
	return nil
}

// Prepare loads configuration from the selected source before any database is opened.
func Prepare(ctx context.Context, directory, explicit string, options Options) (request *Request, err error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	request = &Request{Options: options}
	defer func() {
		if err != nil {
			err = errors.Join(err, request.Release())
			request = nil
		}
	}()
	if options.Changed || options.Staged {
		directory, explicit, err = request.prepareSource(ctx, directory, explicit)
		if err != nil {
			return request, err
		}
	}
	discovery := DiscoveryOptions{Directory: directory, Explicit: explicit}
	if request.Options.Source != nil && request.Options.Source.Info().Kind != git.SourceWorktree {
		discovery.Boundary = request.Options.Source.Info().Dir
	}
	request.Config, err = Discover(discovery)
	if err != nil {
		return request, err
	}
	selection, err := request.Config.Filter(options)
	if err != nil {
		return request, err
	}
	request.Options.WorkDir = request.Config.Root
	if request.Options.Source != nil {
		info := request.Options.Source.Info()
		relative, err := filepath.Rel(info.Dir, request.Config.Root)
		if err != nil {
			return request, err
		}
		request.Options.WorkDir = filepath.Join(info.RepositoryRoot, relative)
		request.Options.Files = []string{}
		for _, change := range request.Options.Source.Changes() {
			path := filepath.Join(info.RepositoryRoot, filepath.FromSlash(change.Path))
			relative, err := filepath.Rel(request.Options.WorkDir, path)
			if err != nil {
				return request, err
			}
			if filepath.IsLocal(relative) && selection.MatchesPath(filepath.ToSlash(relative)) {
				request.Options.Files = append(request.Options.Files, path)
			}
		}
	}
	return request, nil
}

func (request *Request) prepareSource(ctx context.Context, directory, explicit string) (string, string, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", "", err
	}
	if explicit != "" {
		explicit, err = filepath.Abs(explicit)
		if err != nil {
			return "", "", err
		}
		absolute = filepath.Dir(explicit)
	}
	existing := absolute
	for {
		if _, err := os.Stat(existing); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", "", fmt.Errorf("no existing directory for %q", absolute)
		}
		existing = parent
	}
	canonical, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(existing, absolute)
	if err != nil {
		return "", "", err
	}
	absolute = filepath.Join(canonical, relative)
	kind := git.SourceWorktree
	if request.Options.Staged {
		kind = git.SourceStaged
	} else if request.Options.Revision != "" {
		kind = git.SourceCommit
	}
	cache := &git.Cache{Root: filepath.Join(canonical, ".tmp", "uir-sources")}
	request.Options.Source, err = cache.PrepareSource(ctx, git.SourceOptions{Directory: canonical, Kind: kind, Revision: request.Options.Revision})
	if err != nil {
		return "", "", err
	}
	info := request.Options.Source.Info()
	if !insideSource(absolute, info.RepositoryRoot) {
		return "", "", fmt.Errorf("directory %q is outside %q", absolute, info.RepositoryRoot)
	}
	relative, err = filepath.Rel(info.RepositoryRoot, absolute)
	if err != nil {
		return "", "", err
	}
	directory = filepath.Join(info.Dir, relative)
	if explicit != "" {
		explicit = filepath.Join(directory, filepath.Base(explicit))
	}
	return directory, explicit, nil
}

func insideSource(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && filepath.IsLocal(relative)
}

func (request *Request) Empty() bool {
	return request.Options.Files != nil && len(request.Options.Files) == 0
}
func (request *Request) Run(ctx context.Context, database *gorm.DB) *Result {
	return Run(ctx, database, request.Config, request.Options)
}
func (request *Request) Release() error {
	if request.Options.Source != nil {
		return request.Options.Source.Release()
	}
	return nil
}
