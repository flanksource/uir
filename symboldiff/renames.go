package symboldiff

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

type referenceInfo struct {
	diagnostics []error
	tokens      *token.FileSet
	info        *types.Info
	objects     map[*fileSide]map[int]types.Object
}

type referenceImporter struct {
	refs     *referenceInfo
	files    map[string][]*ast.File
	packages map[string]*types.Package
	checking map[string]bool
}

func (loader *referenceImporter) Import(path string) (*types.Package, error) {
	if pkg := loader.packages[path]; pkg != nil {
		return pkg, nil
	}
	files, found := loader.files[path]
	if !found {
		return importer.Default().Import(path)
	}
	if loader.checking[path] {
		return nil, fmt.Errorf("cyclic reference package %s", path)
	}
	loader.checking[path] = true
	config := types.Config{Importer: loader, Error: func(err error) { loader.refs.diagnostics = append(loader.refs.diagnostics, err) }}
	pkg, err := config.Check(path, loader.refs.tokens, files, loader.refs.info)
	delete(loader.checking, path)
	if err != nil {
		return pkg, err
	}
	loader.packages[path] = pkg
	return pkg, nil
}

func resolveReferences(files []changedFile, side int) referenceInfo {
	r := referenceInfo{tokens: token.NewFileSet(), info: &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}, objects: map[*fileSide]map[int]types.Object{}}
	packages := map[string][]*ast.File{}
	documents := map[string]*fileSide{}
	for _, file := range files {
		doc := file.sides()[side]
		if doc == nil || doc.source == nil || doc.content.Excluded != "" {
			continue
		}
		parsed, err := parser.ParseFile(r.tokens, doc.path, doc.source, 0)
		if err != nil {
			continue
		}
		packages[doc.content.PackagePath] = append(packages[doc.content.PackagePath], parsed)
		documents[doc.path] = doc
	}
	loader := referenceImporter{refs: &r, files: packages, packages: map[string]*types.Package{}, checking: map[string]bool{}}
	for name := range packages {
		// Partial type information is intentional: only resolved objects prove a reference.
		if _, err := loader.Import(name); err != nil && len(r.diagnostics) == 0 {
			r.diagnostics = append(r.diagnostics, err)
		}
	}
	for ident, obj := range r.info.Defs {
		pos := r.tokens.Position(ident.Pos())
		doc := documents[pos.Filename]
		if doc == nil {
			continue
		}
		if r.objects[doc] == nil {
			r.objects[doc] = map[int]types.Object{}
		}
		r.objects[doc][pos.Offset] = obj
	}
	return r
}

func (r referenceInfo) spans(entry *entry) [][2]int {
	object := r.objects[entry.file][entry.symbol.NameBytes[0]]
	if object == nil {
		return nil
	}
	var spans [][2]int
	for ident, obj := range r.info.Uses {
		if obj == object {
			pos := r.tokens.Position(ident.Pos())
			if pos.Filename == entry.file.path {
				spans = append(spans, [2]int{pos.Offset, pos.Offset + len(ident.Name)})
			}
		}
	}
	return spans
}

func renameFingerprint(entry *entry, references referenceInfo) string {
	if !entry.decoded() || entry.file.source == nil {
		return ""
	}
	kind := entry.symbol.Kind
	if kind != "func" && kind != "method" && kind != "type" && kind != "struct" && kind != "interface" {
		return ""
	}
	extent := entry.symbol.ExtentBytes
	source := entry.file.source[extent[0]:extent[1]]
	positions := map[int]bool{entry.symbol.NameBytes[0] - extent[0]: true}
	for _, span := range references.spans(entry) {
		positions[span[0]-extent[0]] = true
	}
	var lex scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(source))
	lex.Init(file, source, nil, 0)
	var normalized strings.Builder
	for {
		pos, kind, literal := lex.Scan()
		if kind == token.EOF {
			break
		}
		if positions[file.Offset(pos)] {
			literal = "$self"
		}
		fmt.Fprintf(&normalized, "%s:%q;", kind, literal)
	}
	owner, _ := ownerAndName(entry.symbol.Identifier)
	return entry.file.content.PackagePath + "|" + kind + "|" + owner + "|" + entry.symbol.Visibility + "|" + normalized.String()
}

func pairRenames(files []changedFile, pairs []*pairing) ([]*pairing, error) {
	refs := [2]referenceInfo{resolveReferences(files, 0), resolveReferences(files, 1)}
	groups := [2]map[string][]*pairing{{}, {}}
	for _, pair := range pairs {
		side := 0
		e := pair.before
		if pair.class == ClassAdded {
			side, e = 1, pair.after
		} else if pair.class != ClassRemoved {
			continue
		}
		key := renameFingerprint(e, refs[side])
		if key != "" {
			groups[side][key] = append(groups[side][key], pair)
		}
	}
	merged := map[*pairing]bool{}
	var result []*pairing
	for key, old := range groups[0] {
		current := groups[1][key]
		if len(old) != 1 || len(current) != 1 {
			continue
		}
		before, after := old[0].before, current[0].after
		_, a := ownerAndName(before.symbol.Identifier)
		_, b := ownerAndName(after.symbol.Identifier)
		if a == b {
			continue
		}
		merged[old[0]], merged[current[0]] = true, true
		result = append(result, &pairing{before: before, after: after, class: ClassRenamed})
	}
	for _, pair := range pairs {
		if !merged[pair] {
			result = append(result, pair)
		}
	}
	return pairRenamedChildren(result), nil
}

func pairRenamedChildren(pairs []*pairing) []*pairing {
	merged := map[*pairing]bool{}
	var children []*pairing
	for _, parent := range pairs {
		if parent.class != ClassRenamed || parent.before.symbol.Kind != "type" {
			continue
		}
		_, oldName := ownerAndName(parent.before.symbol.Identifier)
		_, newName := ownerAndName(parent.after.symbol.Identifier)
		for _, old := range pairs {
			if old.class != ClassRemoved {
				continue
			}
			owner, name := ownerAndName(old.before.symbol.Identifier)
			if owner != oldName || old.before.file.content.PackagePath != parent.before.file.content.PackagePath {
				continue
			}
			var matches []*pairing
			for _, current := range pairs {
				if current.class != ClassAdded {
					continue
				}
				currentOwner, currentName := ownerAndName(current.after.symbol.Identifier)
				if currentOwner == newName && currentName == name && current.after.symbol.Kind == old.before.symbol.Kind && current.after.file.content.PackagePath == parent.after.file.content.PackagePath {
					matches = append(matches, current)
				}
			}
			if len(matches) != 1 {
				continue
			}
			current := matches[0]
			if old.before.symbol.PublicHash != current.after.symbol.PublicHash || old.before.symbol.PrivateHash != current.after.symbol.PrivateHash {
				continue
			}
			merged[old], merged[current] = true, true
			pair := &pairing{before: old.before, after: current.after}
			pair.class = classify(pair)
			children = append(children, pair)
		}
	}
	for _, pair := range pairs {
		if !merged[pair] {
			children = append(children, pair)
		}
	}
	return children
}

func referenceLines(files []changedFile, rows []reportedRow) [2]map[*fileSide]map[int]string {
	selected := [2]map[*fileSide]map[int]string{{}, {}}
	for side := range 2 {
		refs := resolveReferences(files, side)
		replacements := map[*fileSide]map[int]string{}
		for _, row := range rows {
			if !row.visible || row.row.Class != ClassRenamed || row.row.MovedAway {
				continue
			}
			e := row.pair.before
			if side == 1 {
				e = row.pair.after
			}
			object := refs.objects[e.file][e.symbol.NameBytes[0]]
			if object == nil {
				continue
			}
			for ident, obj := range refs.info.Uses {
				if obj != object {
					continue
				}
				pos := refs.tokens.Position(ident.Pos())
				for _, file := range files {
					doc := file.sides()[side]
					if doc == nil || doc.path != pos.Filename {
						continue
					}
					if replacements[doc] == nil {
						replacements[doc] = map[int]string{}
					}
					replacements[doc][pos.Offset] = fmt.Sprintf("$rename:%s:%s", row.pkg, row.row.NameBefore)
				}
			}
		}
		for doc, changes := range replacements {
			selected[side][doc] = map[int]string{}
			var offsets []int
			for offset := range changes {
				offsets = append(offsets, offset)
			}
			sort.Sort(sort.Reverse(sort.IntSlice(offsets)))
			lines := strings.Split(string(doc.source), "\n")
			for _, offset := range offsets {
				line := bytes.Count(doc.source[:offset], []byte("\n"))
				column := offset - bytes.LastIndexByte(doc.source[:offset], '\n') - 1
				end := column
				for end < len(lines[line]) && (lines[line][end] == '_' || lines[line][end] >= 'a' && lines[line][end] <= 'z' || lines[line][end] >= 'A' && lines[line][end] <= 'Z' || lines[line][end] >= '0' && lines[line][end] <= '9' || lines[line][end] >= 128) {
					end++
				}
				lines[line] = lines[line][:column] + changes[offset] + lines[line][end:]
				selected[side][doc][line+1] = lines[line]
			}
		}
	}
	return selected
}
