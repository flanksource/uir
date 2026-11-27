package indexer

import (
	"fmt"

	"github.com/flanksource/uir/storage"
)

// domainCatalog is what the publication declares beyond kind names, checked against the registered
// catalogue; every document must validate against the registered catalogue with the declarations added.
func (publication Publication) domainCatalog(registered storage.DomainCatalog) (storage.DomainCatalog, error) {
	declared := storage.DomainCatalog{Symbols: map[string]storage.JSON{}, Metrics: map[string]map[string]storage.MetricSpec{}, Relationships: map[string]storage.RelationshipKind{}}
	for _, kind := range publication.Kinds {
		if err := storage.EvolveKindMetrics(kind.Name, registered.Metrics[kind.Name], kind.Metrics); err != nil {
			return declared, err
		}
		if len(kind.Metrics) > 0 {
			registered.Metrics[kind.Name], declared.Metrics[kind.Name] = kind.Metrics, kind.Metrics
		}
		if len(kind.Schema) == 0 {
			continue
		}
		if _, err := storage.CompilePropertySchema(kind.Schema); err != nil {
			return declared, fmt.Errorf("kind %s: %w", kind.Name, err)
		}
		if existing, ok := registered.Symbols[kind.Name]; ok {
			same, err := sameJSON(existing, kind.Schema)
			if err != nil {
				return declared, err
			}
			if !same {
				return declared, fmt.Errorf("kind %s has a different registered property schema", kind.Name)
			}
		}
		registered.Symbols[kind.Name], declared.Symbols[kind.Name] = kind.Schema, kind.Schema
	}
	if err := publication.declareRelationships(registered, declared); err != nil {
		return declared, err
	}
	for _, document := range publication.Documents {
		if err := registered.Validate(document.Content); err != nil {
			return declared, fmt.Errorf("document %s: %w", document.PathKey, err)
		}
	}
	return declared, nil
}

// declareRelationships adds the publication's relationship kinds to registered and declared, refusing
// one declared twice or conflicting with its registration.
func (publication Publication) declareRelationships(registered, declared storage.DomainCatalog) error {
	for _, kind := range publication.Relationships {
		if err := storage.ValidateRelationshipKind(kind); err != nil {
			return err
		}
		if _, seen := declared.Relationships[kind.Name]; seen {
			return fmt.Errorf("relationship %s is declared twice", kind.Name)
		}
		if len(kind.Schema) == 0 {
			kind.Schema = storage.JSON(`{}`)
		}
		if existing, ok := registered.Relationships[kind.Name]; ok {
			same, err := sameJSON(existing.Schema, kind.Schema)
			if err != nil {
				return err
			}
			if !same || existing.Type != kind.Type {
				return fmt.Errorf("relationship %s conflicts with the registered type or schema", kind.Name)
			}
		}
		registered.Relationships[kind.Name], declared.Relationships[kind.Name] = kind, kind
	}
	return nil
}
