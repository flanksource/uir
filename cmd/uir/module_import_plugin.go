package main

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/flanksource/clicky/entity"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/producer"
)

// publicationFromPlugin runs the producer plugin options names over the checkout at options.File
// (the working directory when empty) and returns its publication. It never runs over HTTP: the
// checkout path would be read from, and the plugin executed on, the server's disk.
func publicationFromPlugin(ctx context.Context, options moduleImportOptions) (indexer.Publication, error) {
	if detachedRequest(ctx) {
		return indexer.Publication{}, entity.NewStatusError(http.StatusBadRequest, "plugin_not_allowed",
			"an HTTP import takes the publication in the body as publication; plugin imports run only from the command line, because they execute a binary and read a checkout on the server")
	}
	if options.Publication != "" {
		return indexer.Publication{}, invalidPublication("--plugin produces the publication; it cannot be combined with --publication")
	}
	binary, err := producer.Discover(options.Plugin, options.PluginPath)
	if err != nil {
		return indexer.Publication{}, err
	}
	checkout, err := filepath.Abs(cmp.Or(options.File, "."))
	if err != nil {
		return indexer.Publication{}, fmt.Errorf("resolve the checkout %q: %w", options.File, err)
	}
	return producer.Run(ctx, binary, producer.PublicationRequest{Path: checkout, Security: options.Security})
}

// validatePluginFlags rejects plugin-only flags given without --plugin.
func validatePluginFlags(options moduleImportOptions) error {
	if options.Plugin == "" && (options.PluginPath != "" || options.Security) {
		return invalidPublication("--plugin-path and --security apply only with --plugin")
	}
	return nil
}
