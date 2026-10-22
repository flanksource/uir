package migrate

import (
	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/sqlglot-go/migrate/sqlserver"
)

func BuildArtifact(request Request) (model.Artifact, error) {
	input, err := Select(request)
	if err != nil {
		return model.Artifact{}, err
	}
	return sqlserver.BuildArtifact(model.ArtifactRequest{Schemas: request.Target.SQLCatalog.Schemas, Request: input})
}
