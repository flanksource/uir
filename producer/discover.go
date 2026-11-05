// Package producer runs an external publication producer: an executable that answers
// `<bin> uir` as a newline-delimited JSON-RPC 2.0 server on stdin/stdout and returns an
// indexer.Publication. The client is generic; any binary that speaks the protocol is a producer.
package producer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PathEnv lists extra directories (os.PathListSeparator separated) searched for producers.
const PathEnv = "UIR_PRODUCER_PATH"

// Binary is a discovered producer executable.
type Binary struct {
	Name string
	Path string
}

// Discover resolves the executable for plugin name. A configured path wins and must be an
// executable file; otherwise $UIR_PRODUCER_PATH, ./.bin, the uir executable's directory and $PATH
// are searched in that order. A miss names every location searched.
func Discover(name, configured string) (Binary, error) {
	if name == "" {
		return Binary{}, fmt.Errorf("a producer plugin name is required")
	}
	if configured != "" {
		if !isExecutable(configured) {
			return Binary{}, fmt.Errorf("producer %q: configured path %q is not an executable file", name, configured)
		}
		return Binary{Name: name, Path: configured}, nil
	}
	directories := searchDirectories()
	for _, directory := range directories {
		candidate := filepath.Join(directory, executableName(name))
		if isExecutable(candidate) {
			return Binary{Name: name, Path: candidate}, nil
		}
	}
	return Binary{}, fmt.Errorf("producer %q not found; searched %s (set --plugin-path, or %s)",
		name, strings.Join(directories, ", "), PathEnv)
}

func searchDirectories() []string {
	var directories []string
	for _, entry := range filepath.SplitList(os.Getenv(PathEnv)) {
		if entry != "" {
			directories = append(directories, entry)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		directories = append(directories, filepath.Join(cwd, ".bin"))
	}
	if executable, err := os.Executable(); err == nil {
		directories = append(directories, filepath.Dir(executable))
	}
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry != "" {
			directories = append(directories, entry)
		}
	}
	return directories
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}

func fileSHA256(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}
