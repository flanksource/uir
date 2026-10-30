package symboldiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// GitLocator reads a patch's sides from the git repository holding a directory: every blob through one
// git cat-file --batch process, and a worktree diff's new sides, which git does not have, from the
// worktree, hashed by one git hash-object process to prove they are still the patch's bytes. The
// worktree's go.mod files name each file's package.
type GitLocator struct {
	toplevel string
	modules  map[string]string
}

var _ SourceLocator = (*GitLocator)(nil)

// gitCommand builds every git process the package starts; tests count them through it.
var gitCommand = func(ctx context.Context, directory string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, args...)...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return command
}

// blobID is an index line's object id, abbreviated or full.
var blobID = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

func NewGitLocator(ctx context.Context, directory string) (*GitLocator, error) {
	output, err := runGit(ctx, directory, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("a patch's blobs are read from the git repository holding %q: %w", directory, err)
	}
	return &GitLocator{toplevel: strings.TrimSpace(string(output)), modules: map[string]string{}}, nil
}

func runGit(ctx context.Context, directory, stdin string, args ...string) ([]byte, error) {
	return gitRun{directory: directory, stdin: stdin}.output(ctx, args...)
}

// gitRun is one git process: the directory it runs in, its stdin, and environment added to the default.
type gitRun struct {
	directory, stdin string
	env              []string
}

func (run gitRun) output(ctx context.Context, args ...string) ([]byte, error) {
	command := gitCommand(ctx, run.directory, args...)
	command.Stdin = strings.NewReader(run.stdin)
	command.Env = append(command.Env, run.env...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s in %q: %w: %s", strings.Join(args, " "), run.directory, err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

// Read writes every request's id to one git cat-file --batch process and reads its answers in order. A
// new side git does not have is read from the worktree; a missing old side is an error.
func (locator *GitLocator) Read(ctx context.Context, requests []BlobRequest) ([]Blob, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	var input strings.Builder
	for _, request := range requests {
		if !blobID.MatchString(request.ID) || presentBlob(request.ID) == "" {
			return nil, fmt.Errorf("%s side of %s: %q is not the id of a present blob", sideLabel(request), request.Path, request.ID)
		}
		input.WriteString(request.ID + "\n")
	}
	output, err := runGit(ctx, locator.toplevel, input.String(), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	blobs := make([]Blob, len(requests))
	var fallbacks []int
	for index, request := range requests {
		content, missing, err := nextBatchObject(&output, request)
		switch {
		case err != nil:
			return nil, fmt.Errorf("%s side of %s: %w", sideLabel(request), request.Path, err)
		case missing && !request.New:
			return nil, fmt.Errorf("old side of %s: blob %s is missing from the repository", request.Path, request.ID)
		case missing:
			fallbacks = append(fallbacks, index)
		default:
			blobs[index] = Blob{Content: content}
		}
	}
	if len(output) > 0 {
		return nil, fmt.Errorf("git cat-file --batch wrote %d bytes beyond the %d objects asked for", len(output), len(requests))
	}
	return blobs, locator.readWorktree(ctx, requests, fallbacks, blobs)
}

// nextBatchObject consumes one "<oid> <type> <size>\n<content>\n" or "<id> missing\n" answer.
func nextBatchObject(output *[]byte, request BlobRequest) ([]byte, bool, error) {
	header, rest, found := bytes.Cut(*output, []byte("\n"))
	if !found {
		return nil, false, fmt.Errorf("git cat-file --batch ended before answering for blob %s", request.ID)
	}
	fields := strings.Fields(string(header))
	switch {
	case len(fields) == 2 && fields[1] == "missing":
		*output = rest
		return nil, true, nil
	case len(fields) == 2 && fields[1] == "ambiguous":
		return nil, false, fmt.Errorf("blob id %s is ambiguous in the repository; produce the patch with --full-index", request.ID)
	case len(fields) != 3:
		return nil, false, fmt.Errorf("git cat-file --batch answered %q for blob %s", header, request.ID)
	case fields[1] != "blob":
		return nil, false, fmt.Errorf("%s names a %s, not a blob", request.ID, fields[1])
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size+1 > len(rest) || rest[size] != '\n' {
		return nil, false, fmt.Errorf("git cat-file --batch answered %q for blob %s without its %s bytes", header, request.ID, fields[2])
	}
	*output = rest[size+1:]
	return rest[:size], false, nil
}

// readWorktree reads the new sides git does not have from the worktree, and hashes them with one git
// hash-object process: each must still hash to the patch's id.
func (locator *GitLocator) readWorktree(ctx context.Context, requests []BlobRequest, fallbacks []int, blobs []Blob) error {
	if len(fallbacks) == 0 {
		return nil
	}
	var paths strings.Builder
	for _, index := range fallbacks {
		request := requests[index]
		if strings.ContainsAny(request.Path, "\n\r") {
			return fmt.Errorf("worktree file %q cannot be hashed: its path holds a line break", request.Path)
		}
		content, err := os.ReadFile(filepath.Join(locator.toplevel, filepath.FromSlash(request.Path)))
		if err != nil {
			return fmt.Errorf("blob %s of %s is not in the repository, and its worktree file cannot be read: %w", request.ID, request.Path, err)
		}
		blobs[index] = Blob{Content: content, Worktree: true}
		paths.WriteString(request.Path + "\n")
	}
	output, err := runGit(ctx, locator.toplevel, paths.String(), "hash-object", "--stdin-paths")
	if err != nil {
		return err
	}
	hashes := strings.Fields(string(output))
	if len(hashes) != len(fallbacks) {
		return fmt.Errorf("git hash-object answered %d hashes for %d worktree files", len(hashes), len(fallbacks))
	}
	for position, index := range fallbacks {
		if request := requests[index]; !strings.HasPrefix(hashes[position], request.ID) {
			return fmt.Errorf("worktree changed since the patch was produced: %s hashes to %s, the patch's new side is blob %s", request.Path, hashes[position], request.ID)
		}
	}
	return nil
}

// RootKey is the module of the repository's top-level go.mod.
func (locator *GitLocator) RootKey() (string, error) {
	module, err := locator.moduleAt(".")
	if err == nil && module == "" {
		err = fmt.Errorf("repository %s has no top-level go.mod to name the diff's root", locator.toplevel)
	}
	return module, err
}

// PackagePath is the import path of the directory holding file: the nearest enclosing go.mod's module
// path joined with the directory below it.
func (locator *GitLocator) PackagePath(file string) (string, error) {
	for directory := path.Dir(file); ; directory = path.Dir(directory) {
		module, err := locator.moduleAt(directory)
		if err != nil {
			return "", err
		}
		if module != "" {
			relative, err := filepath.Rel(directory, path.Dir(file))
			if err != nil {
				return "", fmt.Errorf("package of %s below module %s: %w", file, module, err)
			}
			return path.Join(module, filepath.ToSlash(relative)), nil
		}
		if directory == "." {
			return "", fmt.Errorf("%s is outside every Go module of %s", file, locator.toplevel)
		}
	}
}

// moduleAt is the module path of the worktree's go.mod in directory, "" when it has none.
func (locator *GitLocator) moduleAt(directory string) (string, error) {
	if module, found := locator.modules[directory]; found {
		return module, nil
	}
	manifest := filepath.Join(locator.toplevel, filepath.FromSlash(directory), "go.mod")
	content, err := os.ReadFile(manifest)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		locator.modules[directory] = ""
		return "", nil
	case err != nil:
		return "", err
	}
	module := modfile.ModulePath(content)
	if module == "" {
		return "", fmt.Errorf("%s has no module line", manifest)
	}
	locator.modules[directory] = module
	return module, nil
}
