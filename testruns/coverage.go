package testruns

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/flanksource/uir/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ParseGoCoverProfile reads a `go test -coverprofile` profile into module-relative blocks. Its paths
// are import paths; every one must lie in modulePath, the root's Go module path. A block a profile
// lists more than once, as -coverpkg profiles do, has its counts summed.
func ParseGoCoverProfile(reader io.Reader, modulePath string) ([]Block, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	merged := map[Block]int64{}
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || line == 1 && strings.HasPrefix(text, "mode:") {
			continue
		}
		block, err := parseProfileLine(text, modulePath)
		if err != nil {
			return nil, fmt.Errorf("coverage profile line %d: %w", line, err)
		}
		count := block.Count
		block.Count = 0
		merged[block] += count
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read the coverage profile: %w", err)
	}
	blocks := make([]Block, 0, len(merged))
	for block, count := range merged {
		block.Count = count
		blocks = append(blocks, block)
	}
	slices.SortFunc(blocks, func(left, right Block) int {
		return cmp.Or(cmp.Compare(left.Path, right.Path), cmp.Compare(left.StartLine, right.StartLine), cmp.Compare(left.StartCol, right.StartCol))
	})
	return blocks, nil
}

// parseProfileLine parses "import/path/file.go:startLine.startCol,endLine.endCol statements count".
func parseProfileLine(text, modulePath string) (Block, error) {
	colon := strings.LastIndex(text, ":")
	fields := strings.Fields(text[colon+1:])
	if colon < 0 || len(fields) != 3 {
		return Block{}, invalid("%q is not file:start,end statements count", text)
	}
	file, found := strings.CutPrefix(text[:colon], modulePath+"/")
	if !found {
		return Block{}, invalid("%q is outside module %s", text[:colon], modulePath)
	}
	var block Block
	block.Path = file
	start, end, found := strings.Cut(fields[0], ",")
	if !found {
		return Block{}, invalid("%q has no block range", text)
	}
	var err error
	if block.StartLine, block.StartCol, err = parsePosition(start); err != nil {
		return Block{}, err
	}
	if block.EndLine, block.EndCol, err = parsePosition(end); err != nil {
		return Block{}, err
	}
	if block.Statements, err = strconv.Atoi(fields[1]); err != nil {
		return Block{}, invalid("statements %q: %v", fields[1], err)
	}
	if block.Count, err = strconv.ParseInt(fields[2], 10, 64); err != nil {
		return Block{}, invalid("count %q: %v", fields[2], err)
	}
	return block, nil
}

func parsePosition(position string) (int, int, error) {
	line, column, found := strings.Cut(position, ".")
	if !found {
		return 0, 0, invalid("position %q is not line.column", position)
	}
	parsedLine, err := strconv.Atoi(line)
	if err != nil {
		return 0, 0, invalid("line %q: %v", line, err)
	}
	parsedColumn, err := strconv.Atoi(column)
	if err != nil {
		return 0, 0, invalid("column %q: %v", column, err)
	}
	return parsedLine, parsedColumn, nil
}

// declaration is a proven declaration of a document that coverage and tests can be attributed to.
type declaration struct {
	handle      int64
	kind        string
	name        string
	packagePath string
	extent      storage.Range
}

// coveredDocument is a document of the run's snapshot with its proven declarations.
type coveredDocument struct {
	ordinal      int64
	declarations []declaration
}

// coverageKey is one row of test_coverage within a run and test.
type coverageKey struct {
	document int64
	handle   int64
}

type coverageCount struct{ statements, covered int }

// snapshotDocuments loads, once per import, the snapshot's documents and decodes those a block or a
// test names, keyed by path.
type snapshotDocuments struct {
	database *gorm.DB
	snapshot uuid.UUID
	active   map[string]storage.ActiveDocument
	decoded  map[string]coveredDocument
}

func newSnapshotDocuments(ctx context.Context, database *gorm.DB, snapshot uuid.UUID) (*snapshotDocuments, error) {
	active, err := storage.ActiveDocuments(ctx, database, snapshot, storage.ActiveDocumentOptions{})
	if err != nil {
		return nil, err
	}
	return &snapshotDocuments{database: database, snapshot: snapshot, active: active, decoded: map[string]coveredDocument{}}, nil
}

// document decodes the document the snapshot serves at path, or reports false when it serves none.
func (documents *snapshotDocuments) document(ctx context.Context, path string) (coveredDocument, bool, error) {
	if decoded, found := documents.decoded[path]; found {
		return decoded, true, nil
	}
	active, found := documents.active[path]
	if !found {
		return coveredDocument{}, false, nil
	}
	contents, err := storage.DocumentContents(ctx, documents.database, []uuid.UUID{active.Document.ID})
	if err != nil {
		return coveredDocument{}, false, err
	}
	var content storage.DocumentContent
	if err := json.Unmarshal(contents[active.Document.ID], &content); err != nil {
		return coveredDocument{}, false, fmt.Errorf("decode the document for %q: %w", path, err)
	}
	var ids []string
	for _, symbol := range content.Symbols {
		if symbol.ID != nil {
			ids = append(ids, *symbol.ID)
		}
	}
	handles, err := storage.SymbolHandles(ctx, documents.database, ids)
	if err != nil {
		return coveredDocument{}, false, err
	}
	decoded := coveredDocument{ordinal: active.Document.Ordinal}
	for _, symbol := range content.Symbols {
		if symbol.ID != nil {
			decoded.declarations = append(decoded.declarations, declaration{handle: handles[*symbol.ID], kind: symbol.Kind, name: symbol.Identifier.Method, packagePath: symbol.Identifier.Package, extent: symbol.Extent})
		}
	}
	documents.decoded[path] = decoded
	return decoded, true, nil
}

// enclosing is the innermost declaration whose extent's lines hold [start, end], or file scope when
// none does. A declaration whose lines the range only partly overlaps is an error: the block straddles
// a declaration boundary. Extents are compared by line only, because a coverage profile counts columns
// in bytes and a document in UTF-16 units.
func (document coveredDocument) enclosing(start, end int) (int64, error) {
	best, bestSpan := storage.FileScope, -1
	for _, declared := range document.declarations {
		first, last := declared.extent[0], declared.extent[2]
		switch {
		case end < first || start > last:
			continue
		case start < first || end > last:
			return 0, fmt.Errorf("lines %d-%d cross the boundary of the declaration at lines %d-%d", start, end, first, last)
		}
		if span := last - first; bestSpan < 0 || span < bestSpan {
			best, bestSpan = declared.handle, span
		}
	}
	return best, nil
}

// attribute partitions blocks into the declarations of the snapshot's documents. A block whose path
// the snapshot does not serve, or that straddles a declaration boundary, fails with its position.
func (documents *snapshotDocuments) attribute(ctx context.Context, blocks []Block) (map[coverageKey]coverageCount, error) {
	counts := map[coverageKey]coverageCount{}
	for _, block := range blocks {
		document, found, err := documents.document(ctx, block.Path)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, invalid("coverage block %s names a path snapshot %s does not serve", block, documents.snapshot)
		}
		handle, err := document.enclosing(block.StartLine, block.EndLine)
		if err != nil {
			return nil, invalid("coverage block %s: %v", block, err)
		}
		key := coverageKey{document: document.ordinal, handle: handle}
		count := counts[key]
		count.statements += block.Statements
		if block.Count > 0 {
			count.covered += block.Statements
		}
		counts[key] = count
	}
	return counts, nil
}
