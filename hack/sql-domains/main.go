package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

const root = "example.org/orders"

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(publication()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func publication() indexer.Publication {
	result := indexer.Publication{RootKey: root, Name: "Orders domains", Location: indexer.ExternalLocation{URI: "example://orders"}, Revision: "v1",
		Kinds: []indexer.KindDeclaration{
			{Name: "http.route", Category: symbolhandle.CategoryMember, Schema: storage.JSON(`{"type":"object","properties":{"method":{"type":"string"},"path":{"type":"string"},"description":{"type":"string"}},"required":["method","path"],"additionalProperties":false}`)},
			{Name: "messaging.topic", Category: symbolhandle.CategoryMember},
			{Name: "messaging.consumer", Category: symbolhandle.CategoryCallable},
			{Name: "k8s.service", Category: symbolhandle.CategoryMember},
			{Name: "k8s.pod", Category: symbolhandle.CategoryMember},
		},
	}
	for _, name := range []string{"http.handled_by", "messaging.publishes_to", "messaging.delivers_to", "k8s.selects", "k8s.uses_config"} {
		result.Relationships = append(result.Relationships, storage.RelationshipKind{Name: name, Type: uir.RelationshipTypeReference})
	}
	names := []string{"Orders", "Handle", "OrderCreated", "Consume", "checkout", "pod"}
	kinds := []string{"http.route", "func", "messaging.topic", "messaging.consumer", "k8s.service", "k8s.pod"}
	for i, name := range names {
		identity := indexer.Identity{ModuleKey: root, PackagePath: root, Kind: kinds[i], Name: name}
		result.Symbols = append(result.Symbols, indexer.PublishedSymbol{Identity: identity, Visibility: "exported"})
		result.Documents = append(result.Documents, document(identity))
	}
	result.Documents[0].Content.Symbols[0].Props = map[string]any{"method": "POST", "path": "/orders/new"}
	result.Documents[0].Content.Symbols[0].Payload = storage.JSON(`{"producer":"example","original":{"method":"POST","path":"/orders/new"}}`)
	for i, kind := range []string{"http.handled_by", "messaging.publishes_to", "messaging.delivers_to"} {
		addRelationship(&result, i, i+1, kind)
	}
	addRelationship(&result, 4, 5, "k8s.selects")
	addRelationship(&result, 4, 5, "k8s.uses_config")
	return result
}

func document(identity indexer.Identity) indexer.PublishedDocument {
	text := identity.Name + "\n"
	id := indexer.SymbolID(identity)
	identifier := uir.Identifier{Module: root, Package: root, Method: identity.Name, NodeType: uir.NodeTypeFunction}
	shape := identity.Kind + " " + identity.Name
	span := storage.Range{1, 1, 1, len(identity.Name) + 1}
	bytes := storage.ByteSpan{0, len(identity.Name)}
	return indexer.PublishedDocument{PathKey: identity.Name + ".txt", PackagePath: root, ContentHash: hash(text), Source: text,
		Content: storage.DocumentContent{Version: storage.DocumentFormatVersion, PackagePath: root,
			Symbols: []storage.DocumentSymbol{{ID: &id, Key: identifier.IdentityKey(), Kind: identity.Kind, Visibility: "exported", Shape: shape,
				ShapeHash: hash(shape), BodyHash: hash(text), Name: span, NameBytes: bytes, Extent: span, ExtentBytes: bytes, Identifier: identifier}},
			Occurrences: []storage.DocumentOccurrence{}, Diagnostics: []storage.DocumentDiagnostic{},
		},
	}
}

func addRelationship(result *indexer.Publication, from, to int, kind string) {
	document := &result.Documents[from]
	declaration := document.Content.Symbols[0]
	document.Content.Relationships = append(document.Content.Relationships, storage.DocumentRelationship{
		Source: indexer.SymbolID(result.Symbols[from].Identity), Targets: []string{indexer.SymbolID(result.Symbols[to].Identity)}, Kind: kind, Resolution: "static",
		Evidence: storage.RelationshipEvidence{Range: declaration.Name, Bytes: declaration.NameBytes, Text: result.Symbols[from].Name},
	})
}

func hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
