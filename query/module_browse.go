package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
)

type ModuleSourceView struct {
	ID          string `json:"id"`
	RootKey     string `json:"root_key"`
	Location    string `json:"location"`
	SnapshotID  string `json:"snapshot_id"`
	Path        string `json:"path"`
	PackagePath string `json:"package_path"`
	ContentHash string `json:"content_hash"`
	SizeBytes   int64  `json:"size_bytes"`
}

type ModuleCallView struct {
	ToIdentifier  uir.Identifier `json:"to_identifier"`
	Resolvable    bool           `json:"resolvable"`
	StatementPath string         `json:"statement_path"`
	Line          *int           `json:"line,omitempty"`
	Text          string         `json:"text"`
}

type ModuleNodeView struct {
	ID             string           `json:"id"`
	SourceID       string           `json:"source_id"`
	Path           string           `json:"path"`
	Symbol         string           `json:"symbol"`
	NodeType       string           `json:"node_type"`
	Identifier     uir.Identifier   `json:"identifier"`
	ParentIdentity string           `json:"parent_identity,omitempty"`
	ChildSlot      string           `json:"child_slot"`
	Ordinal        int              `json:"ordinal"`
	Payload        json.RawMessage  `json:"payload"`
	SemanticHash   string           `json:"semantic_hash"`
	Field          *storage.Field   `json:"field,omitempty"`
	Line           *int             `json:"line,omitempty"`
	EndLine        *int             `json:"end_line,omitempty"`
	Column         *int             `json:"column,omitempty"`
	Calls          []ModuleCallView `json:"calls"`
}

type ModuleBrowseResult struct {
	Sources []ModuleSourceView `json:"sources"`
	Nodes   []ModuleNodeView   `json:"nodes"`
}

func (pipeline *Pipeline) BrowseModules(ctx context.Context, options ModuleScopeOptions) (ModuleBrowseResult, error) {
	if pipeline == nil || pipeline.database == nil {
		return ModuleBrowseResult{}, errors.New("UIR query database is required")
	}
	if options.Location != "" && options.SnapshotID != "" {
		return ModuleBrowseResult{}, errors.New("location and snapshot selectors are mutually exclusive")
	}
	if options.RootKey == "" && options.Location == "" && options.SnapshotID == "" {
		return ModuleBrowseResult{}, errors.New("module browse requires root, location, or snapshot")
	}
	scopes, err := pipeline.moduleScopes(ctx, options, false)
	if err != nil {
		return ModuleBrowseResult{}, err
	}
	if len(scopes) != 1 {
		return ModuleBrowseResult{}, fmt.Errorf("module browse requires one snapshot, matched %d", len(scopes))
	}
	scope := scopes[0]
	documents, err := pipeline.scopeDocuments(ctx, scope)
	if err != nil {
		return ModuleBrowseResult{}, err
	}
	result := ModuleBrowseResult{Sources: []ModuleSourceView{}, Nodes: []ModuleNodeView{}}
	for _, document := range documents {
		result.Sources = append(result.Sources, moduleSourceView(scope, document.path, document.source))
		result.Nodes = append(result.Nodes, moduleNodeViews(document)...)
	}
	sort.Slice(result.Nodes, func(i, j int) bool {
		left, right := result.Nodes[i], result.Nodes[j]
		return left.Symbol+"\x00"+left.Path+"\x00"+left.ID < right.Symbol+"\x00"+right.Path+"\x00"+right.ID
	})
	return result, nil
}

func moduleNodeViews(document scopeDocument) []ModuleNodeView {
	calls := make(map[string][]ModuleCallView)
	for _, occurrence := range document.content.Occurrences {
		if !occurrence.IsCall() {
			continue
		}
		calls[occurrence.EnclosingKey] = append(calls[occurrence.EnclosingKey], ModuleCallView{
			ToIdentifier: *occurrence.Target, Resolvable: occurrence.Resolvable,
			StatementPath: occurrence.StatementPath, Line: &occurrence.Range[0], Text: occurrence.Text,
		})
	}
	sourceID := document.source.ID.String()
	nodes := make([]ModuleNodeView, 0, len(document.content.Symbols))
	for _, symbol := range document.content.Symbols {
		if !symbol.Explorable() {
			continue
		}
		outgoing := calls[symbol.Key]
		if outgoing == nil {
			outgoing = []ModuleCallView{}
		}
		line, endLine, column := symbolPosition(symbol)
		nodes = append(nodes, ModuleNodeView{
			ID: sourceID + ":" + symbol.Key, SourceID: sourceID, Path: document.path,
			Symbol: symbol.Identifier.SymbolKey(), NodeType: string(symbol.Identifier.GetNodeType()),
			Identifier: symbol.Identifier, ParentIdentity: symbol.ParentKey, ChildSlot: symbol.ChildSlot,
			Ordinal: symbol.Ordinal, Payload: json.RawMessage(symbol.Payload), SemanticHash: symbol.SemanticHash,
			Field: symbol.Field, Line: line, EndLine: endLine, Column: column, Calls: outgoing,
		})
	}
	return nodes
}

func moduleSourceView(scope moduleScope, path string, revision storage.SourceRevision) ModuleSourceView {
	return ModuleSourceView{
		ID: revision.ID.String(), RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath,
		SnapshotID: scope.snapshot.ID.String(), Path: path, PackagePath: revision.PackagePath,
		ContentHash: revision.ContentHash, SizeBytes: revision.SizeBytes,
	}
}
