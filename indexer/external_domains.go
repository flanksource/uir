package indexer

import (
	"fmt"
	"github.com/flanksource/uir/storage"
)

func (publication Publication) domainCatalog(registered storage.DomainCatalog) (storage.DomainCatalog, error) {
	for _, kind := range publication.Kinds {
		if err := storage.EvolveKindMetrics(kind.Name, registered.Metrics[kind.Name], kind.Metrics); err != nil {
			return registered, err
		}
		if len(kind.Metrics) > 0 {
			registered.Metrics[kind.Name] = kind.Metrics
		}
		if len(kind.Schema) == 0 {
			continue
		}
		if _, err := storage.CompilePropertySchema(kind.Schema); err != nil {
			return registered, fmt.Errorf("kind %s: %w", kind.Name, err)
		}
		if existing, ok := registered.Symbols[kind.Name]; ok {
			same, err := sameJSON(existing, kind.Schema)
			if err != nil {
				return registered, err
			}
			if !same {
				return registered, fmt.Errorf("kind %s has a different registered property schema", kind.Name)
			}
		}
		registered.Symbols[kind.Name] = kind.Schema
	}
	seen := map[string]bool{}
	for _, kind := range publication.Relationships {
		if err := storage.ValidateRelationshipKind(kind); err != nil {
			return registered, err
		}
		if seen[kind.Name] {
			return registered, fmt.Errorf("relationship %s is declared twice", kind.Name)
		}
		seen[kind.Name] = true
		if len(kind.Schema) == 0 {
			kind.Schema = storage.JSON(`{}`)
		}
		if existing, ok := registered.Relationships[kind.Name]; ok {
			same, err := sameJSON(existing.Schema, kind.Schema)
			if err != nil {
				return registered, err
			}
			if !same || existing.Type != kind.Type {
				return registered, fmt.Errorf("relationship %s conflicts with the registered type or schema", kind.Name)
			}
		}
		registered.Relationships[kind.Name] = kind
	}
	for _, document := range publication.Documents {
		if err := registered.Validate(document.Content); err != nil {
			return registered, fmt.Errorf("document %s: %w", document.PathKey, err)
		}
	}
	return registered, nil
}
