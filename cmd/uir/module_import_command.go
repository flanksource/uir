package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/indexer"
	"github.com/spf13/cobra"
)

// moduleImportOptions names the publication to import: a JSON file, or - for stdin, on the CLI, and the
// publication object itself in an HTTP request body.
type moduleImportOptions struct {
	File        string `args:"true"`
	Publication string `flag:"publication" help:"Publication JSON; the field an HTTP request carries the publication in"`
}

func registerImportCommand(root *cobra.Command) {
	var command *cobra.Command
	command = clicky.AddNamedCommandWithContext("import", root, moduleImportOptions{}, func(ctx context.Context, options moduleImportOptions) (indexer.ModuleResult, error) {
		publication, err := readPublication(ctx, options, command.InOrStdin())
		if err != nil {
			return indexer.ModuleResult{}, err
		}
		database, err := databaseFor(ctx)
		if err != nil {
			return indexer.ModuleResult{}, err
		}
		result, err := indexer.Publish(ctx, database, publication)
		if errors.Is(err, indexer.ErrInvalidPublication) {
			return indexer.ModuleResult{}, invalidPublication(err.Error())
		}
		return result, err
	})
	command.Use = "import [file.json|-]"
	command.Short = "Publish a non-Go producer's symbols and documents from a publication JSON file"
	command.Args = cobra.MaximumNArgs(1)
	setModuleRoute(command, "modules/import")
}

func invalidPublication(message string) error {
	return entity.NewStatusError(http.StatusBadRequest, "invalid_publication", message)
}

// readPublication decodes the publication an import names, refusing unknown fields: on the CLI the file
// argument, stdin for -, or --publication; over HTTP only the body's publication field, since a file
// argument would read the server's disk.
func readPublication(ctx context.Context, options moduleImportOptions, stdin io.Reader) (indexer.Publication, error) {
	encoded, source, err := publicationBytes(ctx, options, stdin)
	if err != nil {
		return indexer.Publication{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var publication indexer.Publication
	if err := decoder.Decode(&publication); err != nil {
		return indexer.Publication{}, invalidPublication(fmt.Sprintf("decode the publication from %s: %v", source, err))
	}
	return publication, nil
}

func publicationBytes(ctx context.Context, options moduleImportOptions, stdin io.Reader) ([]byte, string, error) {
	if detachedRequest(ctx) {
		if options.File != "" {
			return nil, "", invalidPublication("an HTTP import takes the publication in the body as publication, not a file")
		}
		if options.Publication == "" {
			return nil, "", invalidPublication("an HTTP import requires the publication in the body as publication")
		}
		return []byte(options.Publication), "the request body", nil
	}
	switch {
	case options.File != "" && options.Publication != "":
		return nil, "", invalidPublication("import takes a publication file or --publication, not both")
	case options.Publication != "":
		return []byte(options.Publication), "--publication", nil
	case options.File == "":
		return nil, "", invalidPublication("import requires a publication JSON file, or - for stdin")
	case options.File == "-":
		encoded, err := io.ReadAll(stdin)
		if err != nil {
			return nil, "", fmt.Errorf("read the publication from stdin: %w", err)
		}
		return encoded, "stdin", nil
	}
	encoded, err := os.ReadFile(options.File)
	if err != nil {
		return nil, "", fmt.Errorf("read the publication %q: %w", options.File, err)
	}
	return encoded, fmt.Sprintf("%q", options.File), nil
}
