package main

import (
	"fmt"
	"log"

	model "github.com/flanksource/sqlglot-go/migrate"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/sql/migrate"
)

func main() {
	name := model.Name{Schema: "dbo", Name: "orders"}
	target := model.Schema{Tables: []model.Table{{Name: name, Columns: []model.Column{{Name: "label", Type: "nvarchar(20)", Nullable: true}, {Name: "target_only", Type: "int", Nullable: true}}}}}
	reference := model.Schema{Tables: []model.Table{{Name: name, Columns: []model.Column{{Name: "label", Type: "nvarchar(100)", Nullable: true}}}}}
	left, err := migrate.Enrich(uir.UIR{}, reference, []string{"dbo"})
	if err != nil {
		log.Fatal(err)
	}
	right, err := migrate.Enrich(uir.UIR{}, target, []string{"dbo"})
	if err != nil {
		log.Fatal(err)
	}
	artifact, err := migrate.BuildArtifact(migrate.Request{Reference: left, Target: right, Selections: []migrate.Selection{{Kind: "column", Object: name, Member: "label"}}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Artifact SHA-256: %s\n", artifact.Hash)
	for _, step := range artifact.Plan.Steps {
		fmt.Println(step.SQL)
	}
}
