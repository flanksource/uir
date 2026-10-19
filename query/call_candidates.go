package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/flanksource/uir"
)

// CallCandidateRequest selects authored call sites by name in one immutable snapshot.
type CallCandidateRequest struct {
	SnapshotID string
	Names      []string
}

// CallCandidate is a call site before a consumer applies its runtime dispatch rules.
type CallCandidate struct {
	SnapshotID  string
	Path        string
	PackagePath string
	SourceHash  string
	Enclosing   uir.Identifier
	Target      uir.Identifier
	Line        int
	StartByte   int
	EndByte     int
	Text        string
}

// FindCallCandidates reads call sites from the selected snapshot's active documents.
// The caller supplies a resolver at runtime; the snapshot stores authored facts only.
func (pipeline *Pipeline) FindCallCandidates(ctx context.Context, request CallCandidateRequest) ([]CallCandidate, error) {
	if pipeline == nil || pipeline.database == nil {
		return nil, fmt.Errorf("UIR query database is required")
	}
	if request.SnapshotID == "" || len(request.Names) == 0 {
		return nil, fmt.Errorf("call candidate search requires a snapshot and names")
	}
	wanted := make(map[string]bool, len(request.Names))
	for _, name := range request.Names {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("call candidate name cannot be blank")
		}
		wanted[name] = true
	}
	selection, err := pipeline.moduleScopes(ctx, ModuleScopeOptions{SnapshotID: request.SnapshotID}, false)
	if err != nil {
		return nil, err
	}
	if len(selection.scopes) != 1 {
		return nil, fmt.Errorf("call candidate search requires one snapshot, matched %d", len(selection.scopes))
	}
	documents, err := pipeline.scopeDocuments(ctx, selection.scopes[0])
	if err != nil {
		return nil, err
	}
	results := make([]CallCandidate, 0)
	seen := map[string]bool{}
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		owners := make(map[string]uir.Identifier, len(document.content.Symbols))
		for _, symbol := range document.content.Symbols {
			owners[symbol.Key] = symbol.Identifier
		}
		for _, call := range document.content.Occurrences {
			if !call.IsCall() || !wanted[call.Target.GetName()] {
				continue
			}
			key := fmt.Sprintf("%s\x00%d\x00%d\x00%s", document.path, call.Bytes[0], call.Bytes[1], call.Target.IdentityKey())
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, CallCandidate{
				SnapshotID: request.SnapshotID, Path: document.path, PackagePath: document.content.PackagePath,
				SourceHash: document.source.ContentHash, Enclosing: owners[call.EnclosingKey], Target: *call.Target,
				Line: call.Range[0], StartByte: call.Bytes[0], EndByte: call.Bytes[1], Text: call.Text,
			})
		}
	}
	slices.SortFunc(results, func(a, b CallCandidate) int {
		return strings.Compare(fmt.Sprintf("%s\x00%012d", a.Path, a.StartByte), fmt.Sprintf("%s\x00%012d", b.Path, b.StartByte))
	})
	return results, nil
}
