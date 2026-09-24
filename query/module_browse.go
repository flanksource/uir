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
	ToRootKey     *string        `json:"to_root_key,omitempty"`
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
	Field          json.RawMessage  `json:"field,omitempty"`
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
	revisions, err := storage.EffectiveSources(ctx, pipeline.database, scope.snapshot.ID)
	if err != nil {
		return ModuleBrowseResult{}, err
	}
	paths := make([]string, 0, len(revisions))
	for path := range revisions {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := ModuleBrowseResult{Sources: []ModuleSourceView{}, Nodes: []ModuleNodeView{}}
	for _, path := range paths {
		revision := revisions[path]
		var projection sourceProjection
		if err := json.Unmarshal(revision.Projection, &projection); err != nil {
			return ModuleBrowseResult{}, fmt.Errorf("decode projection for %s at %s: %w", path, scope.snapshot.ID, err)
		}
		if projection.PackagePath != revision.PackagePath {
			return ModuleBrowseResult{}, fmt.Errorf("projection for %s has package %q, expected %q", path, projection.PackagePath, revision.PackagePath)
		}
		result.Sources = append(result.Sources, moduleSourceView(scope, path, revision))
		calls := make(map[string][]ModuleCallView)
		for _, call := range projection.Calls {
			calls[call.FromIdentity] = append(calls[call.FromIdentity], ModuleCallView{
				ToIdentifier: call.ToIdentifier, ToRootKey: call.ToRootKey, Resolvable: call.Resolvable,
				StatementPath: call.StatementPath, Line: call.StartLine, Text: call.Text,
			})
		}
		for _, node := range projection.Nodes {
			identity := node.Identifier.IdentityKey()
			outgoing := calls[identity]
			if outgoing == nil {
				outgoing = []ModuleCallView{}
			}
			result.Nodes = append(result.Nodes, ModuleNodeView{
				ID: revision.ID.String() + ":" + identity, SourceID: revision.ID.String(), Path: path,
				Symbol: node.Identifier.SymbolKey(), NodeType: string(node.Identifier.GetNodeType()),
				Identifier: node.Identifier, ParentIdentity: node.ParentIdentity, ChildSlot: node.ChildSlot,
				Ordinal: node.Ordinal, Payload: node.Payload, SemanticHash: node.SemanticHash, Field: node.Field,
				Line: node.StartLine, EndLine: node.EndLine, Column: node.Column, Calls: outgoing,
			})
		}
	}
	sort.Slice(result.Nodes, func(i, j int) bool {
		left, right := result.Nodes[i], result.Nodes[j]
		return left.Symbol+"\x00"+left.Path+"\x00"+left.ID < right.Symbol+"\x00"+right.Path+"\x00"+right.ID
	})
	return result, nil
}

func moduleSourceView(scope moduleScope, path string, revision storage.SourceRevision) ModuleSourceView {
	return ModuleSourceView{
		ID: revision.ID.String(), RootKey: scope.root.RootKey, Location: scope.location.CanonicalPath,
		SnapshotID: scope.snapshot.ID.String(), Path: path, PackagePath: revision.PackagePath,
		ContentHash: revision.ContentHash, SizeBytes: revision.SizeBytes,
	}
}
