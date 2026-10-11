package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage/symbolhandle"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DomainCatalog struct {
	Symbols       map[string]JSON             `json:"symbols"`
	Relationships map[string]RelationshipKind `json:"relationships"`
}

func LoadDomainCatalog(ctx context.Context, database *gorm.DB) (DomainCatalog, error) {
	catalog := DomainCatalog{Symbols: map[string]JSON{}, Relationships: map[string]RelationshipKind{}}
	var symbols []SymbolPropertySchema
	if err := database.WithContext(ctx).Order("name").Find(&symbols).Error; err != nil {
		return catalog, fmt.Errorf("load symbol property schemas: %w", err)
	}
	for _, row := range symbols {
		catalog.Symbols[row.Name] = row.Schema
	}
	var relationships []RelationshipKind
	if err := database.WithContext(ctx).Order("name").Find(&relationships).Error; err != nil {
		return catalog, fmt.Errorf("load relationship kinds: %w", err)
	}
	for _, row := range relationships {
		catalog.Relationships[row.Name] = row
	}
	return catalog, nil
}

func RegisterDomainCatalog(ctx context.Context, database *gorm.DB, catalog DomainCatalog) error {
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for name, schema := range catalog.Symbols {
			if _, err := KindCode(ctx, tx, name); err != nil {
				return err
			}
			if len(schema) == 0 {
				continue
			}
			if _, err := CompilePropertySchema(schema); err != nil {
				return fmt.Errorf("kind %s: %w", name, err)
			}
			row := SymbolPropertySchema{Name: name, Schema: schema}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
			var stored SymbolPropertySchema
			if err := tx.Where("name = ?", name).First(&stored).Error; err != nil {
				return err
			}
			if !samePropertySchema(stored.Schema, schema) {
				return fmt.Errorf("kind %s is registered with a different property schema", name)
			}
		}
		for name, kind := range catalog.Relationships {
			if name != kind.Name {
				return fmt.Errorf("relationship catalog key %q differs from name %q", name, kind.Name)
			}
			if err := ValidateRelationshipKind(kind); err != nil {
				return err
			}
			if len(kind.Schema) == 0 {
				kind.Schema = JSON(`{}`)
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&kind).Error; err != nil {
				return err
			}
			var stored RelationshipKind
			if err := tx.Where("name = ?", name).First(&stored).Error; err != nil {
				return err
			}
			if stored.Type != kind.Type || !samePropertySchema(stored.Schema, kind.Schema) {
				return fmt.Errorf("relationship %s is already registered with a different type or schema", name)
			}
		}
		return nil
	})
}

func ValidateRelationshipKind(kind RelationshipKind) error {
	if err := symbolhandle.ValidateCustomKind(kind.Name, symbolhandle.CategoryMember); err != nil {
		return fmt.Errorf("relationship kind: %w", err)
	}
	allowed := []uir.RelationshipType{uir.RelationshipTypeImport, uir.RelationshipTypeCall, uir.RelationshipTypeReference, uir.RelationshipTypeInheritance, uir.RelationshipTypeImplements, uir.RelationshipTypeIncludes, uir.RelationshipTypeForeignKey, uir.RelationshipTypeRead, uir.RelationshipTypeWrite, uir.RelationshipTypeDispatch}
	if !slices.Contains(allowed, kind.Type) {
		return fmt.Errorf("relationship %s has unknown type %q", kind.Name, kind.Type)
	}
	if len(kind.Schema) > 0 {
		if _, err := CompilePropertySchema(kind.Schema); err != nil {
			return fmt.Errorf("relationship %s: %w", kind.Name, err)
		}
	}
	return nil
}

type localSchemaLoader struct{}

func (localSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("property schemas must use local references, cannot load %q", url)
}

func CompilePropertySchema(data JSON) (*jsonschema.Schema, error) {
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode property schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(localSchemaLoader{})
	const url = "https://uir.local/properties.json"
	if err := compiler.AddResource(url, document); err != nil {
		return nil, err
	}
	schema, err := compiler.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("compile property schema: %w", err)
	}
	if schema.Types != nil && !slices.Contains(schema.Types.ToStrings(), "object") {
		return nil, fmt.Errorf("property schema must describe an object")
	}
	return schema, nil
}

func PropertyFields(data JSON) (map[string]string, error) {
	schema, err := CompilePropertySchema(data)
	if err != nil {
		return nil, err
	}
	fields := map[string]string{}
	var walk func(*jsonschema.Schema, string, int) error
	walk = func(current *jsonschema.Schema, prefix string, depth int) error {
		if depth > 32 {
			return fmt.Errorf("property schema nesting at %q exceeds 32 levels", prefix)
		}
		if current.Ref != nil {
			if err := walk(current.Ref, prefix, depth+1); err != nil {
				return err
			}
		}
		for _, part := range current.AllOf {
			if err := walk(part, prefix, depth+1); err != nil {
				return err
			}
		}
		for name, property := range current.Properties {
			field := strings.TrimPrefix(prefix+"."+name, ".")
			typed := property
			for hops := 0; typed.Types == nil && typed.Ref != nil && hops < 32; hops++ {
				typed = typed.Ref
			}
			kind := ""
			if typed.Types != nil {
				for _, value := range typed.Types.ToStrings() {
					if value != "null" {
						if kind != "" {
							kind = "mixed"
							break
						}
						kind = value
					}
				}
			}
			fields[field] = kind
			if err := walk(property, field, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(schema, "", 0); err != nil {
		return nil, err
	}
	return fields, nil
}

func samePropertySchema(left, right JSON) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

func (catalog DomainCatalog) Validate(content DocumentContent) error {
	for _, symbol := range content.Symbols {
		schema, ok := catalog.Symbols[symbol.Kind]
		if !ok || len(schema) == 0 {
			if len(symbol.Props) > 0 {
				return fmt.Errorf("symbol %s has properties but kind %s declares no schema", symbol.Key, symbol.Kind)
			}
			continue
		}
		compiled, err := CompilePropertySchema(schema)
		if err != nil {
			return err
		}
		props := symbol.Props
		if props == nil {
			props = map[string]any{}
		}
		if err := compiled.Validate(props); err != nil {
			return fmt.Errorf("symbol %s properties: %w", symbol.Key, err)
		}
	}
	for i, relationship := range content.Relationships {
		kind, ok := catalog.Relationships[relationship.Kind]
		if !ok {
			return fmt.Errorf("relationship %d uses unregistered kind %q", i, relationship.Kind)
		}
		if len(kind.Schema) == 0 {
			continue
		}
		compiled, err := CompilePropertySchema(kind.Schema)
		if err != nil {
			return err
		}
		props := relationship.Props
		if props == nil {
			props = map[string]any{}
		}
		if err := compiled.Validate(props); err != nil {
			return fmt.Errorf("relationship %d properties: %w", i, err)
		}
	}
	return nil
}
