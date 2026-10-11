package storage

import (
	"fmt"
	"slices"
)

func validateRelationships(content DocumentContent) error {
	if content.Version < 4 {
		if len(content.Relationships) > 0 {
			return fmt.Errorf("native relationships require document format 4")
		}
		for _, symbol := range content.Symbols {
			if len(symbol.Props) > 0 {
				return fmt.Errorf("symbol properties require document format 4")
			}
		}
	}
	for i, relationship := range content.Relationships {
		if len(relationship.Source) != 64 || relationship.Kind == "" {
			return fmt.Errorf("relationship %d requires a source id and kind", i)
		}
		if !slices.Contains([]string{"static", "dynamic", "ambiguous", "unresolved"}, relationship.Resolution) {
			return fmt.Errorf("relationship %d has unknown resolution %q", i, relationship.Resolution)
		}
		if relationship.Resolution == "static" && len(relationship.Targets) != 1 || relationship.Resolution == "ambiguous" && len(relationship.Targets) < 2 || relationship.Resolution == "unresolved" && len(relationship.Targets) > 0 {
			return fmt.Errorf("relationship %d target count contradicts %s resolution", i, relationship.Resolution)
		}
		if len(relationship.Targets) == 0 && relationship.Reference == "" {
			return fmt.Errorf("relationship %d without targets requires a reference", i)
		}
		seen := map[string]bool{}
		for _, target := range relationship.Targets {
			if len(target) != 64 || seen[target] {
				return fmt.Errorf("relationship %d has an invalid or repeated target %q", i, target)
			}
			seen[target] = true
		}
		if err := validRange(relationship.Evidence.Range); err != nil {
			return fmt.Errorf("relationship %d evidence: %w", i, err)
		}
		if err := validSpan(relationship.Evidence.Bytes); err != nil {
			return fmt.Errorf("relationship %d evidence: %w", i, err)
		}
	}
	return nil
}
