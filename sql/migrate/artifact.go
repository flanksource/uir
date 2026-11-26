package migrate

import "github.com/flanksource/sqlglot-go/migrate/engine"

// BuildArtifact plans the selected migration offline and returns the reviewed
// artifact: the changes, the dependents they force to be rebuilt, the ordered
// units of SQL and the hash binding them to the target and the selected desired
// schema. Dependents come from the target catalogue.
func BuildArtifact(request Request) (engine.Artifact, error) {
	_, artifact, err := plan(request)
	return artifact, err
}
