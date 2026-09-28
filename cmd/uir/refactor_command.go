package main

import (
	"fmt"
	"go/token"
	"path/filepath"

	"github.com/flanksource/uir/query"
)

type refactorOptions struct {
	Snapshot    string `flag:"snapshot" required:"true"`
	Source      string `flag:"source" required:"true"`
	Node        string `flag:"node"`
	Action      string `flag:"action" required:"true"`
	NewName     string `flag:"new-name"`
	Destination string `flag:"destination"`
	PreviewHash string `flag:"preview-hash"`
}

func refactorArgs(options refactorOptions, source query.ModuleSourceView, node *query.ModuleNodeView, dsn, schema string) ([]string, error) {
	if source.Path == "" || !filepath.IsLocal(source.Path) || filepath.Ext(source.Path) != ".go" {
		return nil, fmt.Errorf("invalid indexed Go source path %q", source.Path)
	}
	if node == nil {
		if options.Action == "rename" {
			if options.Destination != "" || options.NewName == "" || filepath.Base(options.NewName) != options.NewName || filepath.Ext(options.NewName) != ".go" {
				return nil, fmt.Errorf("file rename requires a new .go basename, got %q", options.NewName)
			}
			return []string{"move", "file", source.Path, filepath.ToSlash(filepath.Join(filepath.Dir(source.Path), options.NewName))}, nil
		}
		if options.Action != "move" || options.NewName != "" || !filepath.IsLocal(options.Destination) || filepath.Ext(options.Destination) != ".go" {
			return nil, fmt.Errorf("file move requires a module-relative .go destination, got %q", options.Destination)
		}
		return []string{"move", "file", source.Path, options.Destination}, nil
	}
	identifier := node.Identifier
	if source.PackagePath == "" {
		return nil, fmt.Errorf("indexed source %q has no package path", source.Path)
	}
	kind, target := "", ""
	switch {
	case identifier.Field != "" && identifier.Type != "" && identifier.Method == "":
		kind, target = "field", identifier.Type+"."+identifier.Field
	case identifier.Method != "" && identifier.Type != "":
		kind, target = "method", identifier.Type+"."+identifier.Method
	case identifier.Method != "":
		kind, target = "func", identifier.Method
	case identifier.Type != "":
		kind, target = "type", identifier.Type
	default:
		return nil, fmt.Errorf("symbol %q is not supported by gopatch rename or move", node.ID)
	}
	if options.Action == "rename" {
		if options.Destination != "" || !token.IsIdentifier(options.NewName) {
			return nil, fmt.Errorf("symbol rename requires a Go identifier, got %q", options.NewName)
		}
		if dsn == "" {
			return nil, fmt.Errorf("UIR index DSN is required for gopatch symbol rename")
		}
		args := []string{"rename", kind, target, options.NewName, "--package", source.PackagePath, "--uir-dsn", dsn}
		if schema != "" {
			args = append(args, "--uir-schema", schema)
		}
		return append(args, "./..."), nil
	}
	if options.Action != "move" || options.NewName != "" || kind == "field" || !filepath.IsLocal(options.Destination) || filepath.Ext(options.Destination) != ".go" {
		return nil, fmt.Errorf("%s move requires a module-relative .go destination, got %q", kind, options.Destination)
	}
	return []string{"move", kind, target, options.Destination, "--package", source.PackagePath}, nil
}
