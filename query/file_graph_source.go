package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/graph"
	"github.com/flanksource/uir/storage"
)

// packageNotes describe the group of every node a graph source made, for exclusion patterns.
type packageNotes interface {
	notePackage(path, module string, builtin bool)
	packageFacts(group string) graphPackageFacts
}

// standalonePackages describe the groups of a file drawn without an index: a group the file does
// not declare a function in is external.
type standalonePackages struct {
	declared map[string]bool
	packages map[string]graphPackageFacts
}

func (notes standalonePackages) notePackage(path, module string, builtin bool) {
	if _, known := notes.packages[path]; !known {
		notes.packages[path] = graphPackageFacts{path: path, module: module, builtin: builtin, external: !builtin && !notes.declared[path]}
	}
}

func (notes standalonePackages) packageFacts(group string) graphPackageFacts {
	facts, known := notes.packages[group]
	if !known {
		panic(fmt.Sprintf("query: graph group %q belongs to no node the file graph source made", group))
	}
	return facts
}

// fileGraphSource serves the graph of one file, alone or over an index. File functions are the
// nodes of a graph.FromRelationships source over the file's relationships. With an index, a callee
// the file does not declare resolves by its query name: one match is the index symbol, where the walk
// continues through indexGraphSource, several are each drawn as an ambiguous dispatch, and none stays
// an unresolved leaf. A file function shadows its indexed copy, the same-named indexed symbol: the
// index's steps to the copy reach the file function, and the copy's calls are the file's.
type fileGraphSource struct {
	file    graph.Source
	lowered *loweredFile
	index   *indexGraphSource
	notes   packageNotes
	facts   map[string]graphPackageFacts
	// copies are the indexed copy of each file function that has one, and shadowed their reverse.
	copies   map[string]string
	shadowed map[string]string
	matches  map[string][]ModuleSymbol
	out      map[string][]graph.Step
}

func newFileGraphSource(ctx context.Context, lowered *loweredFile, index *indexGraphSource) (*fileGraphSource, error) {
	file, err := graph.FromRelationships(graph.RelationshipOptions{Nodes: lowered.nodes(), Relationships: lowered.relationships, Describe: lowered.describe})
	if err != nil {
		return nil, err
	}
	source := &fileGraphSource{
		file: file, lowered: lowered, index: index, copies: map[string]string{}, shadowed: map[string]string{},
		matches: map[string][]ModuleSymbol{}, out: map[string][]graph.Step{},
	}
	if index == nil {
		notes := standalonePackages{declared: lowered.groups, packages: map[string]graphPackageFacts{}}
		source.notes, source.facts = notes, notes.packages
		return source, nil
	}
	source.notes, source.facts = index, index.packages
	for _, function := range lowered.functions {
		copies, err := source.lookup(ctx, function.identifier, uir.RelationshipTypeCall)
		if err != nil {
			return nil, err
		}
		if len(copies) == 1 {
			id := function.identifier.IdentityKey()
			source.copies[id], source.shadowed[copies[0].ID] = copies[0].ID, id
		}
	}
	return source, nil
}

func (source *fileGraphSource) Describe(ctx context.Context, id string) (graph.Node, error) {
	if source.index != nil && !source.lowered.declares(id) {
		return source.index.Describe(ctx, id)
	}
	return source.fileNode(ctx, id)
}

// fileNode is a node the file source made, its group noted.
func (source *fileGraphSource) fileNode(ctx context.Context, id string) (graph.Node, error) {
	node, err := source.file.Describe(ctx, id)
	if err != nil {
		return graph.Node{}, err
	}
	source.note(node)
	return node, nil
}

func (source *fileGraphSource) note(node graph.Node) {
	source.notes.notePackage(node.Group, source.lowered.module(node.Group), node.Group == builtinGroup)
}

func (source *fileGraphSource) Out(ctx context.Context, id string) ([]graph.Step, error) {
	if source.index != nil && !source.lowered.declares(id) {
		steps, err := source.index.Out(ctx, id)
		if err != nil {
			return nil, err
		}
		return source.shadow(ctx, steps)
	}
	return source.fileOut(ctx, id)
}

// fileOut are the steps a file function's body makes, each undeclared callee resolved in the index.
func (source *fileGraphSource) fileOut(ctx context.Context, id string) ([]graph.Step, error) {
	if steps, known := source.out[id]; known {
		return steps, nil
	}
	drawn, err := source.file.Out(ctx, id)
	if err != nil {
		return nil, err
	}
	var steps []graph.Step
	for _, step := range drawn {
		if source.index == nil || !source.lowered.resolvable[step.Node.ID] {
			source.note(step.Node)
			steps = append(steps, step)
			continue
		}
		resolved, err := source.resolveStep(ctx, step)
		if err != nil {
			return nil, err
		}
		steps = append(steps, resolved...)
	}
	source.out[id] = steps
	return steps, nil
}

// resolveStep draws a step to an undeclared target as the steps to what its query name matches in
// the index: the one match, every match of an ambiguous name as a dispatch with status=ambiguous, or
// the unresolved leaf when nothing matches.
func (source *fileGraphSource) resolveStep(ctx context.Context, step graph.Step) ([]graph.Step, error) {
	symbols, err := source.lookup(ctx, step.Node.Identifier, step.Edge.Type)
	if err != nil {
		return nil, err
	}
	if len(symbols) == 0 {
		source.note(step.Node)
		return []graph.Step{step}, nil
	}
	if err := source.index.remember(ctx, symbols); err != nil {
		return nil, err
	}
	steps := make([]graph.Step, 0, len(symbols))
	for _, symbol := range symbols {
		edge := step.Edge
		if len(symbols) > 1 {
			edge.Properties = map[string]string{"status": string(graph.ResolutionAmbiguous)}
			if edge.Type == uir.RelationshipTypeCall {
				edge.Type = uir.RelationshipTypeDispatch
			}
		}
		steps = append(steps, graph.Step{Node: source.index.nodes[symbol.ID], Edge: edge})
	}
	return steps, nil
}

// lookup are the index symbols an identifier's query name names that a step of kind may reach: a
// callable for a call or dispatch, a data symbol for a read or write.
func (source *fileGraphSource) lookup(ctx context.Context, identifier uir.Identifier, kind uir.RelationshipType) ([]ModuleSymbol, error) {
	name := identifierQueryName(identifier)
	called := kind == uir.RelationshipTypeCall || kind == uir.RelationshipTypeDispatch
	key := name + "\x00data"
	if called {
		key = name + "\x00call"
	}
	symbols, known := source.matches[key]
	if !known {
		var err error
		if symbols, err = source.index.index.resolve(ctx, name, func(row storage.Symbol) bool {
			symbol := ModuleSymbol{Kind: row.Kind}
			if called {
				return source.index.index.callable(symbol)
			}
			return source.index.index.dataSymbol(symbol)
		}); err != nil {
			return nil, err
		}
		source.matches[key] = symbols
	}
	return slices.Clone(symbols), nil
}

// shadow points the index's steps to an indexed copy of a file function at the file function.
func (source *fileGraphSource) shadow(ctx context.Context, steps []graph.Step) ([]graph.Step, error) {
	for position, step := range steps {
		if id, shadowed := source.shadowed[step.Node.ID]; shadowed {
			node, err := source.fileNode(ctx, id)
			if err != nil {
				return nil, err
			}
			steps[position].Node = node
		}
	}
	return steps, nil
}

// In are a file function's callers in the file and the index callers of its indexed copy, and an
// index symbol's callers in the index and in the file. The file alone says what its functions call,
// so an index step from the indexed copy of a file function is left out.
func (source *fileGraphSource) In(ctx context.Context, id string) ([]graph.Step, error) {
	if source.index == nil || source.lowered.declares(id) {
		steps, err := source.file.In(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, step := range steps {
			source.note(step.Node)
		}
		if source.index == nil || source.copies[id] == "" {
			return steps, nil
		}
		indexed, err := source.indexCallers(ctx, source.copies[id])
		return append(steps, indexed...), err
	}
	steps, err := source.indexCallers(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, function := range source.lowered.functions {
		callerID := function.identifier.IdentityKey()
		out, err := source.fileOut(ctx, callerID)
		if err != nil {
			return nil, err
		}
		for _, step := range out {
			if step.Node.ID != id {
				continue
			}
			caller, err := source.fileNode(ctx, callerID)
			if err != nil {
				return nil, err
			}
			steps = append(steps, graph.Step{Node: caller, Edge: step.Edge})
		}
	}
	return steps, nil
}

// indexCallers are the index's callers of an index symbol other than the indexed copies of file
// functions.
func (source *fileGraphSource) indexCallers(ctx context.Context, id string) ([]graph.Step, error) {
	steps, err := source.index.In(ctx, id)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(steps, func(step graph.Step) bool {
		_, shadowed := source.shadowed[step.Node.ID]
		return shadowed
	}), nil
}
