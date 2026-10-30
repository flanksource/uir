package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/query"
	"github.com/spf13/cobra"
)

// moduleStatus is the index health queries no longer repeat: the registered locations they skip
// for lacking a current head.
type moduleStatus struct {
	MissingHeads []query.MissingHead `json:"missing_heads"`
}

func (status moduleStatus) Pretty() api.Text {
	if len(status.MissingHeads) == 0 {
		return clicky.Text("Every registered location has a current head", uir.StyleMuted)
	}
	text := clicky.Text(fmt.Sprintf("%d registered locations without a current head", len(status.MissingHeads)), uir.StyleBold)
	for _, missing := range status.MissingHeads {
		text = text.NewLine().Add(icons.Warning).Space().Append(missing.RootKey, uir.StyleWarning).Space().
			Append(missing.Location).Space().Append(missing.Message, uir.StyleMuted)
	}
	return text
}

func registerModuleStatusCommand(root *cobra.Command) {
	command := clicky.AddNamedCommandWithContext("status", root, struct{}{}, func(ctx context.Context, _ struct{}) (moduleStatus, error) {
		database, err := databaseFor(ctx)
		if err != nil {
			return moduleStatus{}, err
		}
		pipeline, err := query.NewPipeline(database)
		if err != nil {
			return moduleStatus{}, err
		}
		missing, err := pipeline.MissingHeads(ctx)
		if err != nil {
			return moduleStatus{}, err
		}
		return moduleStatus{MissingHeads: missing}, nil
	})
	command.Short = "Report registered locations that queries skip for lacking a current head"
	setModuleRoute(command, "modules/status")
	command.Annotations["clicky/operation-method"] = http.MethodGet
}
