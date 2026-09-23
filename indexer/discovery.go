package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

type discoveredWorkspace struct {
	Roots           []discoveredRoot
	RevisionSetHash string
}

type discoveredRoot struct {
	RootKey        string
	ParentRootKey  string
	MountPath      string
	LocalPath      string
	Kind           string
	Revision       string
	RepositoryURI  string
	ContentSetHash string
	Files          []discoveredFile
}

type discoveredFile struct {
	AbsolutePath string
	PathKey      string
	PackagePath  string
	Content      []byte
	ContentHash  string
	SizeBytes    int64
	ModifiedAt   time.Time
}

func discoverWorkspace(ctx context.Context, options Options) (discoveredWorkspace, error) {
	workspacePath, err := filepath.Abs(options.Path)
	if err != nil {
		return discoveredWorkspace{}, fmt.Errorf("resolve index path %q: %w", options.Path, err)
	}
	info, err := os.Stat(workspacePath)
	if err != nil {
		return discoveredWorkspace{}, fmt.Errorf("stat index path %q: %w", workspacePath, err)
	}
	if !info.IsDir() {
		return discoveredWorkspace{}, fmt.Errorf("UIR index path %q is not a directory", workspacePath)
	}
	rootKey := options.RootKey
	if rootKey == "" {
		rootKey = filepath.Base(workspacePath)
	}
	roots, err := discoverRoots(workspacePath, rootKey)
	if err != nil {
		return discoveredWorkspace{}, err
	}
	for i := range roots {
		if err := populateRoot(ctx, &roots[i], roots, options.IncludeTests); err != nil {
			return discoveredWorkspace{}, err
		}
	}
	return discoveredWorkspace{Roots: roots, RevisionSetHash: hashRootSet(roots)}, nil
}

func discoverRoots(workspacePath, rootKey string) ([]discoveredRoot, error) {
	rootPaths := []string{workspacePath}
	err := filepath.WalkDir(workspacePath, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() || current == workspacePath {
			return nil
		}
		if isGitRoot(current) {
			rootPaths = append(rootPaths, current)
		}
		if shouldSkipRootDiscovery(entry.Name()) {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover roots below %q: %w", workspacePath, err)
	}
	sort.SliceStable(rootPaths, func(i, j int) bool { return len(rootPaths[i]) < len(rootPaths[j]) })
	roots := make([]discoveredRoot, 0, len(rootPaths))
	for _, rootPath := range rootPaths {
		root, err := newDiscoveredRoot(workspacePath, rootPath, rootKey, rootPaths)
		if err != nil {
			return nil, err
		}
		roots = append(roots, root)
	}
	return roots, nil
}

func newDiscoveredRoot(workspacePath, rootPath, rootKey string, rootPaths []string) (discoveredRoot, error) {
	mount, err := filepath.Rel(workspacePath, rootPath)
	if err != nil {
		return discoveredRoot{}, fmt.Errorf("resolve mount path for %q: %w", rootPath, err)
	}
	if mount == "." {
		mount = ""
	}
	mount = filepath.ToSlash(mount)
	key := rootKey
	if mount != "" {
		key += "/" + mount
	}
	kind := "directory"
	if isGitRoot(rootPath) {
		kind = "git"
	}
	parentPath := parentRootPath(rootPath, rootPaths, workspacePath)
	if rootPath != workspacePath && declaredSubmodule(parentPath, rootPath) {
		kind = "git-submodule"
	}
	return discoveredRoot{
		RootKey: key, ParentRootKey: parentRootKey(rootPath, rootPaths, rootKey, workspacePath),
		MountPath: mount, LocalPath: rootPath, Kind: kind,
	}, nil
}

func populateRoot(ctx context.Context, root *discoveredRoot, roots []discoveredRoot, includeTests bool) error {
	root.Revision = gitValue(ctx, root.LocalPath, "rev-parse", "HEAD")
	root.RepositoryURI = gitValue(ctx, root.LocalPath, "remote", "get-url", "origin")
	moduleCache := map[string]string{}
	err := filepath.WalkDir(root.LocalPath, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return rootDirectoryAction(current, entry.Name(), root.LocalPath, roots)
		}
		if filepath.Ext(entry.Name()) != ".go" || (!includeTests && strings.HasSuffix(entry.Name(), "_test.go")) {
			return nil
		}
		file, err := readDiscoveredFile(current, root.LocalPath, entry, moduleCache)
		if err != nil {
			return err
		}
		root.Files = append(root.Files, file)
		return nil
	})
	if err != nil {
		return fmt.Errorf("discover Go sources in root %q: %w", root.RootKey, err)
	}
	sort.Slice(root.Files, func(i, j int) bool { return root.Files[i].PathKey < root.Files[j].PathKey })
	root.ContentSetHash = hashFileSet(root.Files)
	return nil
}

func rootDirectoryAction(current, name, currentRoot string, roots []discoveredRoot) error {
	if current != currentRoot && isNestedRoot(current, currentRoot, roots) {
		return filepath.SkipDir
	}
	if current != currentRoot && shouldSkipDirectory(name) {
		return filepath.SkipDir
	}
	return nil
}

func readDiscoveredFile(current, rootPath string, entry fs.DirEntry, moduleCache map[string]string) (discoveredFile, error) {
	content, err := os.ReadFile(current)
	if err != nil {
		return discoveredFile{}, err
	}
	pathKey, err := filepath.Rel(rootPath, current)
	if err != nil {
		return discoveredFile{}, err
	}
	stat, err := entry.Info()
	if err != nil {
		return discoveredFile{}, err
	}
	return discoveredFile{
		AbsolutePath: current, PathKey: filepath.ToSlash(pathKey),
		PackagePath: packagePath(current, rootPath, moduleCache), Content: content,
		ContentHash: hashBytes(content), SizeBytes: stat.Size(), ModifiedAt: stat.ModTime().UTC(),
	}, nil
}

func packagePath(filePath, rootPath string, cache map[string]string) string {
	directory := filepath.Dir(filePath)
	for current := directory; pathWithin(current, rootPath); current = filepath.Dir(current) {
		if module, ok := cache[current]; ok {
			return joinModulePath(module, current, directory)
		}
		goModPath := filepath.Join(current, "go.mod")
		content, err := os.ReadFile(goModPath)
		if err == nil {
			parsed, parseErr := modfile.Parse(goModPath, content, nil)
			if parseErr == nil && parsed.Module != nil {
				cache[current] = parsed.Module.Mod.Path
				return joinModulePath(parsed.Module.Mod.Path, current, directory)
			}
		}
		if current == rootPath {
			break
		}
	}
	relative, err := filepath.Rel(rootPath, directory)
	if err != nil || relative == "." {
		return filepath.Base(rootPath)
	}
	return filepath.ToSlash(relative)
}

func joinModulePath(module, moduleDirectory, packageDirectory string) string {
	relative, err := filepath.Rel(moduleDirectory, packageDirectory)
	if err != nil || relative == "." {
		return module
	}
	return strings.TrimSuffix(module, "/") + "/" + filepath.ToSlash(relative)
}

func parentRootKey(rootPath string, paths []string, rootKey, workspacePath string) string {
	if rootPath == workspacePath {
		return ""
	}
	parentPath := parentRootPath(rootPath, paths, workspacePath)
	mount, _ := filepath.Rel(workspacePath, parentPath)
	if mount == "." {
		return rootKey
	}
	return rootKey + "/" + filepath.ToSlash(mount)
}

func parentRootPath(rootPath string, paths []string, workspacePath string) string {
	parentPath := workspacePath
	for _, candidate := range paths {
		if candidate == rootPath || !pathWithin(rootPath, candidate) {
			continue
		}
		if len(candidate) > len(parentPath) {
			parentPath = candidate
		}
	}
	return parentPath
}

func declaredSubmodule(parentPath, rootPath string) bool {
	content, err := os.ReadFile(filepath.Join(parentPath, ".gitmodules"))
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(parentPath, rootPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(content), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found && strings.TrimSpace(key) == "path" && filepath.Clean(strings.TrimSpace(value)) == filepath.Clean(relative) {
			return true
		}
	}
	return false
}

func pathWithin(path, parent string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func isNestedRoot(path, currentRoot string, roots []discoveredRoot) bool {
	for _, root := range roots {
		if root.LocalPath == path && root.LocalPath != currentRoot {
			return true
		}
	}
	return false
}

func isGitRoot(path string) bool {
	_, err := os.Lstat(filepath.Join(path, ".git"))
	return err == nil
}

func shouldSkipDirectory(name string) bool {
	return name == ".git" || name == "testdata" || name == "vendor" ||
		(strings.HasPrefix(name, ".") && name != ".") || strings.HasPrefix(name, "_")
}

func shouldSkipRootDiscovery(name string) bool {
	return name == ".git" || name == "testdata" ||
		(strings.HasPrefix(name, ".") && name != ".") || strings.HasPrefix(name, "_")
}

func gitValue(ctx context.Context, directory string, args ...string) string {
	commandArgs := append([]string{"-C", directory}, args...)
	output, err := exec.CommandContext(ctx, "git", commandArgs...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func hashRootSet(roots []discoveredRoot) string {
	items := make([][7]string, len(roots))
	for i := range roots {
		items[i] = [7]string{
			roots[i].RootKey, roots[i].ParentRootKey, roots[i].MountPath, roots[i].Kind,
			roots[i].Revision, roots[i].RepositoryURI, roots[i].ContentSetHash,
		}
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		panic(err)
	}
	return hashBytes(encoded)
}

func hashFileSet(files []discoveredFile) string {
	hasher := sha256.New()
	for _, file := range files {
		_, _ = hasher.Write([]byte(file.PathKey))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(file.PackagePath))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(file.ContentHash))
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func hashBytes(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func validateOptions(options *Options) error {
	if options == nil {
		return errors.New("UIR index options are required")
	}
	options.ProjectKey = strings.TrimSpace(options.ProjectKey)
	if options.ProjectKey == "" {
		return errors.New("UIR index project key is required")
	}
	if options.Path == "" {
		options.Path = "."
	}
	return nil
}
