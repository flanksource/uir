package lint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/uir/query"
	"github.com/flanksource/uir/storage"
)

// SQLCommandOptions provide inputs for running only the migration lint checks.
type SQLCommandOptions struct {
	WorkDir, Dialect   string
	Files              []string
	CompatibilityLevel int
	Stdin              io.Reader
}

// RunSQL applies the same mapped-source and native rule handling as uir lint
// using a temporary UIR index.
func RunSQL(ctx context.Context, options SQLCommandOptions) (_ *linters.LinterResult, returnErr error) {
	config, err := Discover(DiscoveryOptions{Directory: options.WorkDir})
	if errors.Is(err, ErrNoConfig) {
		config = &Config{Version: 1, Root: options.WorkDir, Path: filepath.Join(options.WorkDir, ConfigName)}
	} else if err != nil {
		return nil, err
	}
	sources, err := sqlCommandSources(config, options)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, errors.New("no SQL migration sources matched")
	}
	directory := filepath.Join(config.Root, ".tmp")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	temp, err := os.MkdirTemp(directory, "uir-sql-lint-")
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, os.RemoveAll(temp)) }()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(temp, "index.db")})
	if err != nil {
		return nil, err
	}
	connection, err := database.DB()
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, connection.Close()) }()
	report := Run(ctx, database, config, Options{WorkDir: config.Root, SQLOnly: true, sqlFiles: sources,
		Dialect: options.Dialect, CompatibilityLevel: options.CompatibilityLevel})
	return report.LinterResult, nil
}

func sqlCommandSources(config *Config, options SQLCommandOptions) ([]mappedSource, error) {
	if options.Stdin != nil {
		if len(options.Files) != 0 {
			return nil, errors.New("stdin must be the only SQL source")
		}
		body, err := io.ReadAll(options.Stdin)
		if err != nil {
			return nil, err
		}
		return []mappedSource{{Filename: "stdin.sql", Content: string(body), Package: "migrations", Producer: "sql", Dialect: options.Dialect}}, nil
	}
	if slices.Contains(options.Files, "-") {
		return nil, errors.New("stdin needs a SQL input reader")
	}
	var mapped []mappedSource
	if len(config.Sources) > 0 {
		var err error
		mapped, err = readMappedSources(config.Root, config.Sources)
		if err != nil {
			return nil, err
		}
	}
	if len(options.Files) == 0 && len(config.Sources) > 0 {
		return mapped, nil
	}
	return selectSQLFiles(config.Root, options, mapped)
}

func selectSQLFiles(root string, options SQLCommandOptions, mapped []mappedSource) ([]mappedSource, error) {
	patterns := make([]string, 0, len(options.Files))
	for _, name := range options.Files {
		if !filepath.IsAbs(name) {
			name = filepath.Join(options.WorkDir, name)
		}
		rel, err := filepath.Rel(root, name)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("%s is outside the SQL project %s", name, root)
		}
		if info, err := os.Stat(name); err == nil && info.IsDir() {
			rel = filepath.Join(rel, "**")
		}
		patterns = append(patterns, filepath.ToSlash(rel))
	}
	match, err := query.CompilePathFilter(patterns, nil)
	if err != nil {
		return nil, err
	}
	selected := []mappedSource{}
	seen := map[string]bool{}
	for _, source := range mapped {
		if slices.ContainsFunc(source.Inputs, match) {
			selected = append(selected, source)
			for _, input := range source.Inputs {
				seen[input] = true
			}
		}
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	err = fs.WalkDir(directory.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name != "." && strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".sql") || seen[name] || !match(name) {
			return nil
		}
		source := mappedSource{Filename: name, Origin: name, Inputs: []string{name}, Package: "migrations", Producer: "sql", Dialect: options.Dialect}
		source.Content, err = readSourceFile(directory.FS(), name)
		if err == nil {
			selected = append(selected, source)
		}
		return err
	})
	return selected, errors.Join(err, directory.Close())
}
