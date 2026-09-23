package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type persistRequest struct {
	Project           storage.Project
	Previous          *previousSnapshot
	Workspace         discoveredWorkspace
	Files             []indexedFile
	ConfigurationHash string
	ParsedFiles       int
	ReusedFiles       int
}

type persistNode struct {
	RootKey string
	PathKey string
	Spec    nodeSpec
}

type persistedTarget struct {
	RootKey string
	Node    storage.Node
	ID      uir.Identifier
}

type persistState struct {
	request       persistRequest
	snapshot      storage.Snapshot
	roots         map[string]storage.Root
	sources       map[string]storage.Source
	nodes         map[string]storage.Node
	targets       targetIndex
	locations     map[string]int
	nodeBatches   [3][]storage.Node
	nodeLocations []storage.NodeLocation
	nodeFields    []storage.Field
	relationships int
}

func (indexer *Indexer) persist(ctx context.Context, request persistRequest) (Result, error) {
	state := persistState{request: request}
	err := indexer.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := state.createSnapshot(transaction); err != nil {
			return err
		}
		if err := state.createRoots(transaction); err != nil {
			return err
		}
		if err := state.createSources(transaction); err != nil {
			return err
		}
		if err := state.createNodes(transaction); err != nil {
			return err
		}
		if err := state.createRelationships(transaction); err != nil {
			return err
		}
		return state.publish(transaction)
	})
	if err != nil {
		return Result{}, fmt.Errorf("publish UIR snapshot: %w", err)
	}
	version := int64(1)
	if request.Previous != nil {
		version = request.Previous.Head.Version + 1
	}
	return Result{
		ProjectKey: request.Project.ProjectKey, SnapshotID: state.snapshot.ID.String(), HeadVersion: version,
		Roots: len(request.Workspace.Roots), Files: len(request.Files), ParsedFiles: request.ParsedFiles,
		ReusedFiles: request.ReusedFiles, Nodes: len(state.nodes), Relationships: state.relationships,
	}, nil
}

func (state *persistState) createSnapshot(database *gorm.DB) error {
	state.snapshot = storage.Snapshot{
		ID: uuid.New(), ProjectID: state.request.Project.ID, State: storage.SnapshotBuilding,
		RevisionSetHash: state.request.Workspace.RevisionSetHash, ConfigurationHash: state.request.ConfigurationHash,
		ExtractorVersion: ExtractorVersion, PayloadSchema: "uir-snapshot-v1", DocumentPayload: storage.JSON(`{}`),
		StartedAt: time.Now().UTC(), Properties: jsonValue(map[string]any{
			"identity_key_version": "v1", "indexer": "go/ast",
		}),
	}
	if err := database.Create(&state.snapshot).Error; err != nil {
		return fmt.Errorf("create building snapshot: %w", err)
	}
	return nil
}

func (state *persistState) createRoots(database *gorm.DB) error {
	state.roots = make(map[string]storage.Root, len(state.request.Workspace.Roots))
	for _, discovered := range state.request.Workspace.Roots {
		root := storage.Root{
			ID: uuid.New(), SnapshotID: state.snapshot.ID, RootKey: discovered.RootKey,
			Kind: discovered.Kind, MountPath: discovered.MountPath, ContentSetHash: discovered.ContentSetHash,
			PathCase: "sensitive", NormalizationVersion: "v1", LocalPath: stringPointer(discovered.LocalPath),
			Properties: jsonValue(map[string]any{"discovered_by": "go/ast"}),
		}
		root.RepositoryKey = stringPointer(discovered.RootKey)
		root.RepositoryURI = optionalString(discovered.RepositoryURI)
		root.Revision = optionalString(discovered.Revision)
		if discovered.Kind == "git-submodule" {
			root.SubmodulePath = optionalString(discovered.MountPath)
		}
		if discovered.ParentRootKey != "" {
			parent, ok := state.roots[discovered.ParentRootKey]
			if !ok {
				return fmt.Errorf("root %q has unknown parent %q", discovered.RootKey, discovered.ParentRootKey)
			}
			root.ParentRootID = &parent.ID
		}
		if err := database.Create(&root).Error; err != nil {
			return fmt.Errorf("create root %q: %w", root.RootKey, err)
		}
		state.roots[root.RootKey] = root
	}
	return nil
}

func (state *persistState) createSources(database *gorm.DB) error {
	state.sources = make(map[string]storage.Source, len(state.request.Files))
	sources := make([]storage.Source, 0, len(state.request.Files))
	for _, indexed := range state.request.Files {
		root, ok := state.roots[indexed.Root.RootKey]
		if !ok {
			return fmt.Errorf("source %q has unknown root %q", indexed.File.PathKey, indexed.Root.RootKey)
		}
		modifiedAt := indexed.File.ModifiedAt
		source := storage.Source{
			ID: uuid.New(), RootID: root.ID, PathKey: indexed.File.PathKey, DisplayPath: indexed.File.PathKey,
			Kind: "file", Language: "go", ContentHash: indexed.File.ContentHash,
			SizeBytes: indexed.File.SizeBytes, ModifiedAt: &modifiedAt,
			Properties: jsonValue(map[string]any{
				"package_path": indexed.UIR.PackagePath, "package_name": indexed.UIR.PackageName,
			}),
		}
		sources = append(sources, source)
		state.sources[sourceMapKey(indexed.Root.RootKey, indexed.File.PathKey)] = source
	}
	if len(sources) > 0 {
		if err := database.CreateInBatches(sources, 32).Error; err != nil {
			return fmt.Errorf("create %d snapshot sources: %w", len(sources), err)
		}
	}
	return nil
}

func (state *persistState) createNodes(database *gorm.DB) error {
	requested, err := collectNodes(state.request.Files)
	if err != nil {
		return err
	}
	sort.SliceStable(requested, func(i, j int) bool {
		left, right := nodePriority(requested[i].Spec.Identifier), nodePriority(requested[j].Spec.Identifier)
		if left != right {
			return left < right
		}
		if requested[i].RootKey != requested[j].RootKey {
			return requested[i].RootKey < requested[j].RootKey
		}
		return requested[i].Spec.Identifier.IdentityKey() < requested[j].Spec.Identifier.IdentityKey()
	})
	state.nodes = make(map[string]storage.Node, len(requested))
	state.locations = make(map[string]int, len(requested))
	ordinals := map[string]int{}
	for _, requestedNode := range requested {
		if err := state.createNode(requestedNode, ordinals); err != nil {
			return err
		}
	}
	for priority, nodes := range state.nodeBatches {
		if len(nodes) == 0 {
			continue
		}
		if err := database.CreateInBatches(nodes, 32).Error; err != nil {
			return fmt.Errorf("create %d snapshot nodes at priority %d: %w", len(nodes), priority, err)
		}
	}
	if len(state.nodeLocations) > 0 {
		if err := database.CreateInBatches(state.nodeLocations, 32).Error; err != nil {
			return fmt.Errorf("create %d node locations: %w", len(state.nodeLocations), err)
		}
	}
	if len(state.nodeFields) > 0 {
		if err := database.CreateInBatches(state.nodeFields, 32).Error; err != nil {
			return fmt.Errorf("create %d field projections: %w", len(state.nodeFields), err)
		}
	}
	return nil
}

func (state *persistState) createNode(requested persistNode, ordinals map[string]int) error {
	root, ok := state.roots[requested.RootKey]
	if !ok {
		return fmt.Errorf("node %q has unknown root %q", requested.Spec.Identifier.SymbolKey(), requested.RootKey)
	}
	identityKey := requested.Spec.Identifier.IdentityKey()
	nodeKey := nodeMapKey(requested.RootKey, identityKey)
	if existing, exists := state.nodes[nodeKey]; exists {
		if existing.SemanticHash != requested.Spec.SemanticHash {
			return fmt.Errorf("conflicting declarations for identity %q in root %q have semantic hashes %q and %q", identityKey, requested.RootKey, existing.SemanticHash, requested.Spec.SemanticHash)
		}
		return state.createNodeLocation(requested, existing, false)
	}
	var parentID *uuid.UUID
	if requested.Spec.ParentIdentity != "" {
		parent, found := state.nodes[nodeMapKey(requested.RootKey, requested.Spec.ParentIdentity)]
		if !found {
			return fmt.Errorf("node %q has missing parent identity %q in root %q", identityKey, requested.Spec.ParentIdentity, requested.RootKey)
		}
		parentID = &parent.ID
	}
	ordinalKey := requested.RootKey + "\x00" + requested.Spec.ParentIdentity + "\x00" + requested.Spec.ChildSlot
	node := storageNode(state.snapshot.ID, root.ID, parentID, requested.Spec, ordinals[ordinalKey])
	ordinals[ordinalKey]++
	if err := state.createNodeLocation(requested, node, true); err != nil {
		return err
	}
	if requested.Spec.Field != nil {
		field := *requested.Spec.Field
		field.NodeID = node.ID
		state.nodeFields = append(state.nodeFields, field)
	}
	state.nodeBatches[nodePriority(requested.Spec.Identifier)] = append(state.nodeBatches[nodePriority(requested.Spec.Identifier)], node)
	state.nodes[nodeKey] = node
	state.targets.add(persistedTarget{RootKey: requested.RootKey, Node: node, ID: requested.Spec.Identifier})
	return nil
}

func (state *persistState) createNodeLocation(requested persistNode, node storage.Node, primary bool) error {
	identityKey := requested.Spec.Identifier.IdentityKey()
	source, ok := state.sources[sourceMapKey(requested.RootKey, requested.PathKey)]
	if !ok {
		return fmt.Errorf("node %q has unknown source %q", identityKey, requested.PathKey)
	}
	nodeKey := nodeMapKey(requested.RootKey, identityKey)
	location := storage.NodeLocation{
		ID: uuid.New(), RootID: node.RootID, NodeID: node.ID, SourceID: source.ID,
		Role: "declaration", Ordinal: state.locations[nodeKey], StartLine: requested.Spec.StartLine,
		EndLine: requested.Spec.EndLine, Column: requested.Spec.Column, IsPrimary: primary,
	}
	state.nodeLocations = append(state.nodeLocations, location)
	state.locations[nodeKey]++
	return nil
}

func storageNode(snapshotID, rootID uuid.UUID, parentID *uuid.UUID, spec nodeSpec, ordinal int) storage.Node {
	identifier := spec.Identifier
	return storage.Node{
		ID: uuid.New(), SnapshotID: snapshotID, RootID: rootID, ParentID: parentID,
		ChildSlot: spec.ChildSlot, Ordinal: ordinal, NodeType: string(identifier.GetNodeType()),
		IdentityKey: identifier.IdentityKey(), SymbolKey: identifier.SymbolKey(),
		Module: identifier.Module, Package: identifier.Package, TypeName: identifier.Type,
		Method: identifier.Method, Field: identifier.Field, Signature: identifier.Signature,
		Language: "go", Traits: storage.JSON(`[]`), PayloadSchema: "uir-node-v1",
		Payload: spec.Payload, SemanticHash: spec.SemanticHash, CreatedAt: time.Now().UTC(),
	}
}

func collectNodes(files []indexedFile) ([]persistNode, error) {
	packages := map[string]persistNode{}
	nodes := make([]persistNode, 0)
	for _, file := range files {
		packageID := uir.Identifier{Package: file.UIR.PackagePath, NodeType: uir.NodeTypePackage}
		packageKey := nodeMapKey(file.Root.RootKey, packageID.IdentityKey())
		if _, exists := packages[packageKey]; !exists {
			packageNode, err := newPackageNode(file, packageID)
			if err != nil {
				return nil, err
			}
			packages[packageKey] = packageNode
		}
		for _, spec := range file.UIR.Nodes {
			if spec.ParentIdentity == "" {
				spec.ParentIdentity = packageID.IdentityKey()
			}
			nodes = append(nodes, persistNode{RootKey: file.Root.RootKey, PathKey: file.File.PathKey, Spec: spec})
		}
	}
	for _, packageNode := range packages {
		nodes = append(nodes, packageNode)
	}
	return nodes, nil
}

func newPackageNode(file indexedFile, identifier uir.Identifier) (persistNode, error) {
	builder := uir.NewPackage(file.UIR.PackagePath, identifier).WithLanguage("go")
	builder = builder.WithSource(file.File.PathKey, 1, 1)
	node := builder.Build()
	payload, err := json.Marshal(node)
	if err != nil {
		return persistNode{}, fmt.Errorf("marshal package %q: %w", file.UIR.PackagePath, err)
	}
	line := 1
	return persistNode{
		RootKey: file.Root.RootKey, PathKey: file.File.PathKey,
		Spec: nodeSpec{
			Identifier: identifier, ChildSlot: "packages", Payload: storage.JSON(payload),
			SemanticHash: node.Hash(), StartLine: &line, EndLine: &line,
		},
	}, nil
}

func nodePriority(identifier uir.Identifier) int {
	switch identifier.GetNodeType() {
	case uir.NodeTypePackage:
		return 0
	case uir.NodeTypeType:
		return 1
	default:
		return 2
	}
}

func (state *persistState) createRelationships(database *gorm.DB) error {
	relationships := make([]storage.Relationship, 0, 32)
	for _, indexed := range state.request.Files {
		for _, call := range indexed.UIR.Calls {
			relationship, err := state.createRelationship(indexed, call)
			if err != nil {
				return err
			}
			relationships = append(relationships, relationship)
			if len(relationships) == cap(relationships) {
				if err := database.CreateInBatches(relationships, 32).Error; err != nil {
					return fmt.Errorf("create call relationships: %w", err)
				}
				relationships = relationships[:0]
			}
			state.relationships++
		}
	}
	if len(relationships) > 0 {
		if err := database.CreateInBatches(relationships, 32).Error; err != nil {
			return fmt.Errorf("create call relationships: %w", err)
		}
	}
	return nil
}

func (state *persistState) createRelationship(indexed indexedFile, call callSpec) (storage.Relationship, error) {
	sourceRootKey := call.ToRootKey
	from, ok := state.nodes[nodeMapKey(indexed.Root.RootKey, call.FromIdentity)]
	if !ok {
		return storage.Relationship{}, fmt.Errorf("call %q has unknown source identity %q", call.StatementPath, call.FromIdentity)
	}
	source, ok := state.sources[sourceMapKey(indexed.Root.RootKey, indexed.File.PathKey)]
	if !ok {
		return storage.Relationship{}, fmt.Errorf("call %q has unknown source %q", call.StatementPath, indexed.File.PathKey)
	}
	if call.LocalRoot && call.ToRootKey == nil {
		call.ToRootKey = stringPointer(indexed.Root.RootKey)
	}
	target, resolved := persistedTarget{}, false
	if call.Resolvable {
		target, resolved = state.targets.resolve(call)
	}
	targetIdentifier := call.ToIdentifier
	if resolved {
		targetIdentifier = target.ID
		call.ToRootKey = stringPointer(target.RootKey)
	}
	identifierJSON := jsonValue(targetIdentifier)
	edgeKey := relationshipEdgeKey(indexed.File.PathKey, call)
	relationship := storage.Relationship{
		ID: uuid.New(), SnapshotID: state.snapshot.ID, FromRootID: from.RootID, FromNodeID: from.ID,
		EdgeKey: edgeKey, ToRootKey: call.ToRootKey,
		ToIdentityKey: targetIdentifier.IdentityKey(), ToSymbolKey: targetIdentifier.SymbolKey(),
		ToIdentifier: identifierJSON, RelationshipType: string(uir.RelationshipTypeCall),
		SourceID: &source.ID, StartLine: call.StartLine, EndLine: call.EndLine, Column: call.Column,
		StatementPath: call.StatementPath, Text: call.Text,
		Payload: jsonValue(map[string]any{
			"resolvable": call.Resolvable, "source_identifier": call.ToIdentifier,
			"source_root_key": sourceRootKey, "local_root": call.LocalRoot,
		}),
	}
	if resolved {
		relationship.ToSnapshotID = &state.snapshot.ID
		relationship.ToNodeID = &target.Node.ID
	}
	return relationship, nil
}

func relationshipEdgeKey(pathKey string, call callSpec) string {
	canonical := pathKey + "\x00" + call.FromIdentity + "\x00" + call.StatementPath + "\x00" + call.ToIdentifier.IdentityKey()
	return "call:" + hashBytes([]byte(canonical))[:24]
}

func (state *persistState) publish(database *gorm.DB) error {
	completed := time.Now().UTC()
	if err := database.Model(&storage.Snapshot{}).Where("id = ? AND state = ?", state.snapshot.ID, storage.SnapshotBuilding).
		Updates(map[string]any{"state": storage.SnapshotReady, "completed_at": completed}).Error; err != nil {
		return fmt.Errorf("mark snapshot ready: %w", err)
	}
	if state.request.Previous == nil {
		head := storage.ProjectHead{
			ProjectID: state.request.Project.ID, SnapshotID: state.snapshot.ID, Version: 1, ActivatedAt: completed,
		}
		if err := database.Create(&head).Error; err != nil {
			return fmt.Errorf("publish first project head: %w", err)
		}
		return nil
	}
	previous := state.request.Previous.Head
	update := database.Model(&storage.ProjectHead{}).
		Where("project_id = ? AND snapshot_id = ? AND version = ?", previous.ProjectID, previous.SnapshotID, previous.Version).
		Updates(map[string]any{
			"snapshot_id": state.snapshot.ID, "version": previous.Version + 1, "activated_at": completed,
		})
	if update.Error != nil {
		return fmt.Errorf("advance project head: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return errors.New("project head changed while indexing; retry the reindex")
	}
	return nil
}

func sourceMapKey(rootKey, pathKey string) string { return rootKey + "\x00" + pathKey }

func nodeMapKey(rootKey, identityKey string) string { return rootKey + "\x00" + identityKey }

func stringPointer(value string) *string { return &value }

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func jsonValue(value any) storage.JSON {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return storage.JSON(encoded)
}
