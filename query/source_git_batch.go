package query

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/flanksource/uir/storage"
)

var pinnedGitRevision = regexp.MustCompile(`^[a-fA-F0-9]{40,64}$`)

// gitSources recovers, from the pinned commit of one scope, the sources its checkout and stored blobs
// no longer hold, writing each one's bytes or error into reads at the source's index.
type gitSources struct {
	scope        moduleScope
	resolvedRoot string
	sources      []verifiedSource
	reads        []sourceRead
}

// read recovers the pending sources with one git rev-parse and one git cat-file --batch process.
func (git *gitSources) read(ctx context.Context, pending []int) {
	if len(pending) == 0 {
		return
	}
	commit := git.scope.snapshot.GitCommit
	if commit == "" && git.scope.snapshot.WorktreeState == storage.WorktreeClean {
		commit = git.scope.snapshot.Revision
	}
	if !pinnedGitRevision.MatchString(commit) {
		git.fail(pending, func(source verifiedSource) error {
			return fmt.Errorf("source %q has changed since snapshot %s and no pinned Git revision can recover it", source.path, source.snapshot)
		})
		return
	}
	repoRoot, err := git.repositoryRoot(ctx)
	if err != nil {
		git.fail(pending, func(source verifiedSource) error {
			return fmt.Errorf("locate Git repository for source %q: %w", source.path, err)
		})
		return
	}
	asked, objects := git.objectNames(commit, repoRoot, pending)
	if len(asked) == 0 {
		return
	}
	output, err := catFileBatch(ctx, repoRoot, objects)
	if err != nil {
		git.fail(asked, func(source verifiedSource) error {
			return fmt.Errorf("read Git source %q at %s: %w", source.path, commit, err)
		})
		return
	}
	git.answer(commit, asked, objects, output)
}

func (git *gitSources) repositoryRoot(ctx context.Context) (string, error) {
	top, err := exec.CommandContext(ctx, "git", "-C", git.resolvedRoot, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(strings.TrimSpace(string(top)))
}

// objectNames are the commit:path names cat-file reads for the pending sources, and the indexes of the
// sources asked for; a source that cannot be named gets its error instead.
func (git *gitSources) objectNames(commit, repoRoot string, pending []int) ([]int, []string) {
	var asked []int
	var objects []string
	for _, index := range pending {
		source := git.sources[index]
		modulePath := filepath.Join(git.scope.location.CanonicalPath, filepath.FromSlash(source.path))
		relative, err := filepath.Rel(repoRoot, modulePath)
		switch {
		case err != nil || !filepath.IsLocal(relative):
			git.reads[index].err = fmt.Errorf("source %q escapes Git repository %q", source.path, repoRoot)
		case strings.ContainsAny(relative, "\n\r"):
			git.reads[index].err = fmt.Errorf("source %q cannot be read from Git: its path holds a line break", source.path)
		default:
			asked = append(asked, index)
			objects = append(objects, commit+":"+filepath.ToSlash(relative))
		}
	}
	return asked, objects
}

// answer verifies each blob cat-file answered against its source's indexed hash, in the order asked.
func (git *gitSources) answer(commit string, asked []int, objects []string, output []byte) {
	for position, index := range asked {
		source := git.sources[index]
		content, err := nextGitBlob(&output, objects[position])
		var stream *gitStreamError
		switch {
		case errors.As(err, &stream):
			git.fail(asked[position:], func(source verifiedSource) error {
				return fmt.Errorf("read Git source %q at %s: %w", source.path, commit, stream)
			})
			return
		case err != nil:
			git.reads[index].err = fmt.Errorf("read Git source %q at %s: %w", source.path, commit, err)
		case contentHash(content) != source.hash:
			git.reads[index].err = fmt.Errorf("git source %q at %s does not match indexed content hash", source.path, commit)
		default:
			git.reads[index] = sourceRead{content: content, origin: "git"}
		}
	}
	if len(output) > 0 {
		git.fail(asked, func(source verifiedSource) error {
			return fmt.Errorf("read Git source %q at %s: git cat-file --batch wrote %d bytes beyond the %d objects asked for", source.path, commit, len(output), len(asked))
		})
	}
}

func (git *gitSources) fail(indexes []int, reason func(verifiedSource) error) {
	for _, index := range indexes {
		git.reads[index] = sourceRead{err: reason(git.sources[index])}
	}
}

// catFileBatch writes every object name to one git cat-file --batch process and returns its answers.
func catFileBatch(ctx context.Context, repoRoot string, objects []string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", "-C", repoRoot, "cat-file", "--batch", "-Z")
	command.Stdin = strings.NewReader(strings.Join(objects, "\x00") + "\x00")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git cat-file --batch in %q: %w: %s", repoRoot, err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

// gitStreamError is a cat-file answer that cannot be parsed, after which no later answer can be found.
type gitStreamError struct{ message string }

func (err *gitStreamError) Error() string { return err.message }

// nextGitBlob consumes one "<oid> <type> <size>\x00<content>\x00" answer, or a NUL-delimited answer such as
// "<object> missing" that names why the object has no content.
func nextGitBlob(output *[]byte, object string) ([]byte, error) {
	header, rest, found := bytes.Cut(*output, []byte{0})
	if !found {
		return nil, &gitStreamError{message: fmt.Sprintf("git cat-file --batch ended before answering for %s", object)}
	}
	fields := strings.Fields(string(header))
	if len(fields) != 3 {
		*output = rest
		return nil, fmt.Errorf("git cat-file --batch answered %q", header)
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size < 0 {
		*output = rest
		return nil, fmt.Errorf("git cat-file --batch answered %q", header)
	}
	if size >= len(rest) || rest[size] != 0 {
		return nil, &gitStreamError{message: fmt.Sprintf("git cat-file --batch answered %q for %s without its %d bytes", header, object, size)}
	}
	*output = rest[size+1:]
	if fields[1] != "blob" {
		return nil, fmt.Errorf("%s names a %s, not a blob", object, fields[1])
	}
	return rest[:size], nil
}
