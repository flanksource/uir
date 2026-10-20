package buildconstraint

import (
	"context"
	"fmt"
	"go/build/constraint"
)

const maximumNodes = 65536

type decision struct {
	tag       string
	low, high int
}

type operation struct {
	or          bool
	left, right int
}

type diagram struct {
	ctx     context.Context
	nodes   []decision
	unique  map[decision]int
	applied map[operation]int
	negated map[int]int
}

func newDiagram(ctx context.Context) *diagram {
	return &diagram{ctx: ctx, nodes: make([]decision, 2), unique: map[decision]int{}, applied: map[operation]int{}, negated: map[int]int{}}
}

func (diagram *diagram) node(node decision) (int, error) {
	if err := diagram.ctx.Err(); err != nil {
		return 0, err
	}
	if node.low == node.high {
		return node.low, nil
	}
	if id, found := diagram.unique[node]; found {
		return id, nil
	}
	if len(diagram.unique) >= maximumNodes {
		return 0, fmt.Errorf("build constraint analysis exceeds %d decision nodes", maximumNodes)
	}
	id := len(diagram.nodes)
	diagram.nodes = append(diagram.nodes, node)
	diagram.unique[node] = id
	return id, nil
}

func (diagram *diagram) compile(expression constraint.Expr) (int, error) {
	if err := diagram.ctx.Err(); err != nil {
		return 0, err
	}
	if expression == nil {
		return 1, nil
	}
	switch expression := expression.(type) {
	case *constraint.TagExpr:
		return diagram.node(decision{tag: expression.Tag, low: 0, high: 1})
	case *constraint.NotExpr:
		child, err := diagram.compile(expression.X)
		if err != nil {
			return 0, err
		}
		return diagram.negate(child)
	case *constraint.AndExpr:
		return diagram.binary(false, expression.X, expression.Y)
	case *constraint.OrExpr:
		return diagram.binary(true, expression.X, expression.Y)
	default:
		panic(fmt.Sprintf("unhandled build constraint %T", expression))
	}
}

func (diagram *diagram) binary(or bool, left, right constraint.Expr) (int, error) {
	lhs, err := diagram.compile(left)
	if err != nil {
		return 0, err
	}
	rhs, err := diagram.compile(right)
	if err != nil {
		return 0, err
	}
	return diagram.apply(operation{or: or, left: lhs, right: rhs})
}

func (diagram *diagram) negate(id int) (int, error) {
	if id < 2 {
		return 1 - id, nil
	}
	if opposite, found := diagram.negated[id]; found {
		return opposite, nil
	}
	node := diagram.nodes[id]
	low, err := diagram.negate(node.low)
	if err != nil {
		return 0, err
	}
	high, err := diagram.negate(node.high)
	if err != nil {
		return 0, err
	}
	opposite, err := diagram.node(decision{tag: node.tag, low: low, high: high})
	if err == nil {
		diagram.negated[id] = opposite
	}
	return opposite, err
}

func (diagram *diagram) apply(op operation) (int, error) {
	if err := diagram.ctx.Err(); err != nil {
		return 0, err
	}
	if op.left > op.right {
		op.left, op.right = op.right, op.left
	}
	if op.left == op.right {
		return op.left, nil
	}
	if op.or && op.left == 0 || !op.or && op.left == 1 {
		return op.right, nil
	}
	if op.or && (op.left == 1 || op.right == 1) {
		return 1, nil
	}
	if !op.or && op.left == 0 {
		return 0, nil
	}
	if id, found := diagram.applied[op]; found {
		return id, nil
	}
	left, right := diagram.nodes[op.left], diagram.nodes[op.right]
	tag := min(left.tag, right.tag)
	ll, lh := diagram.branches(op.left, tag)
	rl, rh := diagram.branches(op.right, tag)
	low, err := diagram.apply(operation{or: op.or, left: ll, right: rl})
	if err != nil {
		return 0, err
	}
	high, err := diagram.apply(operation{or: op.or, left: lh, right: rh})
	if err != nil {
		return 0, err
	}
	id, err := diagram.node(decision{tag: tag, low: low, high: high})
	if err == nil {
		diagram.applied[op] = id
	}
	return id, err
}

func (diagram *diagram) branches(id int, tag string) (int, int) {
	if node := diagram.nodes[id]; node.tag == tag {
		return node.low, node.high
	}
	return id, id
}

func (diagram *diagram) without(id int, tag string, seen map[int]bool) bool {
	if id < 2 {
		return id == 1
	}
	if result, found := seen[id]; found {
		return result
	}
	node := diagram.nodes[id]
	if node.tag == tag {
		return node.low != 0
	}
	if node.tag > tag {
		return true
	}
	result := diagram.without(node.low, tag, seen) || diagram.without(node.high, tag, seen)
	seen[id] = result
	return result
}
