package syntax

import (
	"fmt"
	"sort"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

// Document extracts one Go file from its syntax alone, outside any index, and fills each symbol's
// public and private layer hashes and shapes from its extracted node, so two syntax documents of a file
// classify a change by layer. It reads no type information, so no symbol has a canonical id.
func Document(pathKey, packagePath string, content []byte) (storage.DocumentContent, error) {
	indexed, err := Extract(pathKey, packagePath, content)
	if err != nil {
		return storage.DocumentContent{}, err
	}
	nodes := make(map[storage.ByteSpan]uir.Node, len(indexed.Nodes))
	for _, node := range indexed.Nodes {
		nodes[node.Name] = node.node
	}
	if len(nodes) != len(indexed.Nodes) {
		return storage.DocumentContent{}, fmt.Errorf("%q declares %d symbols over %d name spans", pathKey, len(indexed.Nodes), len(nodes))
	}
	document := indexed.Document()
	for index := range document.Symbols {
		symbol := &document.Symbols[index]
		node := nodes[symbol.NameBytes]
		prints := uir.Fingerprint(node)
		symbol.PublicHash, symbol.PrivateHash = prints.Public, prints.Private
		symbol.Shape, symbol.PublicShape = uir.Shape(node, uir.LayerPrivate), uir.Shape(node, uir.LayerPublic)
	}
	return document, nil
}

// Document renders the AST extraction of one file in the document format, symbols sorted by
// extent start and occurrences by start byte; ties keep extraction order.
func (indexed *File) Document() storage.DocumentContent {
	lines := NewLineIndex(indexed.content)
	content := storage.DocumentContent{
		Version: storage.DocumentFormatVersion, PackagePath: indexed.PackagePath,
		Symbols:     make([]storage.DocumentSymbol, 0, len(indexed.Nodes)),
		Occurrences: make([]storage.DocumentOccurrence, 0, len(indexed.Calls)),
		Diagnostics: []storage.DocumentDiagnostic{},
	}
	for _, node := range indexed.Nodes {
		content.Symbols = append(content.Symbols, storage.DocumentSymbol{
			Key: node.Identifier.IdentityKey(), Kind: node.Kind, Visibility: node.Visibility, Shape: node.Shape,
			ContentHash: node.ContentHash, Name: lines.RangeOf(node.Name), NameBytes: node.Name,
			Extent: lines.RangeOf(node.Extent), ExtentBytes: node.Extent, Identifier: node.Identifier,
			ParentKey: node.ParentIdentity, ChildSlot: node.ChildSlot, Ordinal: node.Ordinal,
			Payload: node.Payload, SemanticHash: node.SemanticHash, Field: node.Field, Metrics: node.Metrics,
		})
	}
	for _, call := range indexed.Calls {
		target := call.ToIdentifier
		content.Occurrences = append(content.Occurrences, storage.DocumentOccurrence{
			Role: "call", Range: lines.RangeOf(call.Span), Bytes: call.Span, EnclosingKey: call.FromIdentity,
			Target: &target, Resolvable: call.Resolvable, LocalRoot: call.LocalRoot,
			StatementPath: call.StatementPath, Text: call.Text,
		})
	}
	sort.SliceStable(content.Symbols, func(i, j int) bool {
		left, right := content.Symbols[i].ExtentBytes, content.Symbols[j].ExtentBytes
		return left[0] < right[0] || (left[0] == right[0] && left[1] > right[1])
	})
	sort.SliceStable(content.Occurrences, func(i, j int) bool {
		return content.Occurrences[i].Bytes[0] < content.Occurrences[j].Bytes[0]
	})
	return content
}
