package query

import (
	"context"
	"errors"
	"fmt"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	"gorm.io/gorm"
)

var nodeColumns = map[string]string{
	"node_type":    "node_type",
	"module":       "module",
	"package":      "package",
	"type":         "type_name",
	"method":       "method",
	"field":        "field",
	"signature":    "signature",
	"language":     "language",
	"symbol_key":   "symbol_key",
	"identity_key": "identity_key",
}

func (pipeline *Pipeline) execute(ctx context.Context, resolved *resolution) (Result, error) {
	result := Result{
		Operation: resolved.query.Operation, ProjectKey: resolved.project.ProjectKey,
		SnapshotID: resolved.snapshot.ID.String(), RootKey: displayResultRoot(resolved.root), Stages: resolved.stages,
	}
	switch resolved.query.Operation {
	case OperationNodes:
		return pipeline.executeNodes(ctx, resolved, result)
	case OperationCallers, OperationCallees:
		return pipeline.executeGraph(ctx, resolved, result)
	case OperationUnresolvedCalls:
		return pipeline.executeUnresolved(ctx, resolved, result)
	default:
		return Result{}, fmt.Errorf("execute UIR query: unsupported operation %q", resolved.query.Operation)
	}
}

func (pipeline *Pipeline) executeNodes(ctx context.Context, resolved *resolution, result Result) (Result, error) {
	query := pipeline.nodeQuery(ctx, resolved, "node")
	if err := query.Order("node.root_id, node.identity_key").Limit(resolved.limit).Scan(&result.Nodes).Error; err != nil {
		return Result{}, fmt.Errorf("execute UIR node query: %w", err)
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "execute", Value: fmt.Sprintf("%d nodes", len(result.Nodes))})
	return result, nil
}

func (pipeline *Pipeline) executeGraph(ctx context.Context, resolved *resolution, result Result) (Result, error) {
	target, err := pipeline.resolveTarget(ctx, resolved)
	if err != nil {
		return Result{}, fmt.Errorf("resolve UIR query target: %w", err)
	}
	result.Target = &target
	result.Stages = append(result.Stages, ResolutionStage{Name: "target", Value: target.ID.String()})
	query := pipeline.graphQuery(ctx, resolved, target)
	if err := query.Order("node.root_id, node.identity_key").Limit(resolved.limit).Scan(&result.Nodes).Error; err != nil {
		return Result{}, fmt.Errorf("execute UIR %s query: %w", resolved.query.Operation, err)
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "execute", Value: fmt.Sprintf("%d nodes", len(result.Nodes))})
	return result, nil
}

func (pipeline *Pipeline) resolveTarget(ctx context.Context, resolved *resolution) (storage.Node, error) {
	if len(resolved.predicates) == 0 {
		return storage.Node{}, errors.New("call queries require at least one symbol predicate in addition to root")
	}
	query := pipeline.nodeQuery(ctx, resolved, "node")
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return storage.Node{}, fmt.Errorf("count target candidates: %w", err)
	}
	if count == 0 {
		return storage.Node{}, errors.New("target selector matched no nodes")
	}
	if count != 1 {
		return storage.Node{}, fmt.Errorf("target selector matched %d nodes; add root or structured identifier predicates", count)
	}
	var target storage.Node
	if err := pipeline.nodeQuery(ctx, resolved, "node").First(&target).Error; err != nil {
		return storage.Node{}, fmt.Errorf("load target node: %w", err)
	}
	return target, nil
}

func (pipeline *Pipeline) nodeQuery(ctx context.Context, resolved *resolution, alias string) *gorm.DB {
	query := pipeline.database.WithContext(ctx).Table("uir_nodes AS "+alias).
		Where(alias+".snapshot_id = ?", resolved.snapshot.ID)
	if resolved.root != nil {
		query = query.Where(alias+".root_id = ?", resolved.root.ID)
	}
	for _, predicate := range resolved.predicates {
		query = query.Where(alias+"."+nodeColumns[predicate.Field]+" = ?", predicate.Value)
	}
	return query
}

func (pipeline *Pipeline) graphQuery(ctx context.Context, resolved *resolution, target storage.Node) *gorm.DB {
	query := pipeline.database.WithContext(ctx).Table("uir_relationships AS edge").Select("DISTINCT node.*").
		Where("edge.snapshot_id = ? AND edge.relationship_type = ?", resolved.snapshot.ID, uir.RelationshipTypeCall)
	if resolved.query.Operation == OperationCallers {
		return query.Joins(`JOIN uir_nodes AS node ON node.snapshot_id = edge.snapshot_id AND node.root_id = edge.from_root_id AND node.id = edge.from_node_id`).
			Where("edge.to_node_id = ?", target.ID)
	}
	return query.Joins(`JOIN uir_nodes AS node ON node.snapshot_id = edge.to_snapshot_id AND node.id = edge.to_node_id`).
		Where("edge.from_node_id = ?", target.ID)
}

func (pipeline *Pipeline) executeUnresolved(ctx context.Context, resolved *resolution, result Result) (Result, error) {
	query := pipeline.database.WithContext(ctx).Model(&storage.Relationship{}).
		Where("snapshot_id = ? AND relationship_type = ? AND to_node_id IS NULL", resolved.snapshot.ID, uir.RelationshipTypeCall)
	if resolved.root != nil {
		query = query.Where("from_root_id = ?", resolved.root.ID)
	}
	if err := query.Order("from_root_id, from_node_id, edge_key").Limit(resolved.limit).Find(&result.Relationships).Error; err != nil {
		return Result{}, fmt.Errorf("execute unresolved UIR calls query: %w", err)
	}
	result.Stages = append(result.Stages, ResolutionStage{Name: "execute", Value: fmt.Sprintf("%d relationships", len(result.Relationships))})
	return result, nil
}

func displayResultRoot(root *storage.Root) string {
	if root == nil {
		return ""
	}
	return root.RootKey
}
