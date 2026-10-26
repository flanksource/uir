package indexer

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

// deltaCorpus has the shape of a comparison catalog: modules of packages of two documents each, of
// which a delta round changes one package in only a few modules.
type deltaCorpus struct {
	modules, packages, changed int
}

var comparisonDeltaCorpus = deltaCorpus{modules: 85, packages: 50, changed: 3}

func (corpus deltaCorpus) publication(module int, commit string, bodies map[string]string) Publication {
	publication := deltaFixture(commit, bodies)
	publication.RootKey = fmt.Sprintf("example.org/catalog/m%02d", module)
	publication.Location.URI = "oipa://example/" + publication.RootKey
	publication.Options.GitCommit = fmt.Sprintf("%040x", commitNumber(commit))
	for i := range publication.Documents {
		pkg := publication.RootKey + "/" + publication.Documents[i].PackagePath[len("example.org/catalog/"):]
		publication.Documents[i].PackagePath, publication.Documents[i].Content.PackagePath = pkg, pkg
	}
	return publication
}

func commitNumber(commit string) int {
	var number int
	if _, err := fmt.Sscanf(commit, "c%d", &number); err != nil {
		panic(fmt.Sprintf("commit %q: %v", commit, err))
	}
	return number
}

func (corpus deltaCorpus) bodies(module int, revision string) map[string]string {
	bodies := make(map[string]string, corpus.packages*2)
	for pkg := range corpus.packages {
		bodies[fmt.Sprintf("p%03d/body.xml", pkg)] = fmt.Sprintf("<Body module=\"%d\" package=\"%d\" revision=%q/>\n", module, pkg, revision)
		bodies[fmt.Sprintf("p%03d/metadata.json", pkg)] = fmt.Sprintf(`{"module":%d,"package":%d}`, module, pkg)
	}
	return bodies
}

// round publishes one delta per module against base, changing one package in the first corpus.changed
// modules and nothing in the rest.
func (corpus deltaCorpus) round(ctx context.Context, database *gorm.DB, base []string, commit string) error {
	for module := range corpus.modules {
		var bodies map[string]string
		if module < corpus.changed {
			bodies = map[string]string{"p000/body.xml": fmt.Sprintf("<Body changed=%q/>\n", commit), "p000/metadata.json": `{"changed":true}`}
		}
		publication := corpus.publication(module, commit, bodies)
		publication.Options.BaseSnapshotID = base[module]
		publication.Delta = &PublicationDelta{}
		if _, err := Publish(ctx, database, publication); err != nil {
			return err
		}
	}
	return nil
}

// BenchmarkDeltaPublish times one delta round of a comparison catalog against a published base.
func BenchmarkDeltaPublish(b *testing.B) {
	ctx := context.Background()
	database, err := storage.UirDB(ctx, storage.DBOptions{DSN: filepath.Join(b.TempDir(), "delta.db")})
	if err != nil {
		b.Fatal(err)
	}
	corpus := comparisonDeltaCorpus
	base := make([]string, corpus.modules)
	for module := range corpus.modules {
		result, err := Publish(ctx, database, corpus.publication(module, "c1", corpus.bodies(module, "c1")))
		if err != nil {
			b.Fatal(err)
		}
		base[module] = result.SnapshotID
	}
	b.ReportAllocs()
	commit := 1
	for b.Loop() {
		commit++
		if err := corpus.round(ctx, database, base, fmt.Sprintf("c%d", commit)); err != nil {
			b.Fatal(err)
		}
	}
}
