package lint

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/flanksource/gavel/linters"
	"github.com/flanksource/gavel/models"
	sqlglot "github.com/flanksource/sqlglot-go/sqlglot"
	"github.com/flanksource/sqlglot-go/sqlglot/access"
	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer"
	"github.com/flanksource/uir/indexer/syntax"
	sqlsource "github.com/flanksource/uir/sql"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
	"gorm.io/gorm"
)

type sqlPublication struct {
	indexer.Publication
	symbols  map[string]indexer.PublishedSymbol
	files    map[string]mappedSource
	findings []models.Violation
}

func publishSQL(ctx context.Context, database *gorm.DB, workDir string, sources []mappedSource, options indexer.PublishOptions) (sqlPublication, string, error) {
	pub := sqlPublication{
		Publication: indexer.Publication{
			RootKey: "sql:" + workDir, Name: filepath.Base(workDir), Location: indexer.ExternalLocation{URI: workDir},
			Options: options,
			Kinds: []indexer.KindDeclaration{
				{Name: "sql.script", Category: symbolhandle.CategoryMember, Schema: storage.JSON(`{"type":"object","properties":{"name":{"type":"string"},"phase":{"type":"string"},"labels":{"type":"object"}}}`)},
				{Name: "sql.function", Category: symbolhandle.CategoryCallable},
				{Name: "sql.feature", Category: symbolhandle.CategoryMember, Schema: storage.JSON(`{"type":"object","properties":{"min_compatibility_level":{"type":"integer"}}}`)},
			},
		},
		symbols: map[string]indexer.PublishedSymbol{}, files: map[string]mappedSource{},
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return pub, "", err
		}
		if source.Producer != "sql" {
			return pub, "", fmt.Errorf("%s: no mapped source producer %q", source.Filename, source.Producer)
		}
		if err := pub.addSource(source); err != nil {
			return pub, "", err
		}
	}
	if len(pub.Documents) == 0 {
		return pub, "", nil
	}
	for _, symbol := range pub.symbols {
		pub.Symbols = append(pub.Symbols, symbol)
	}
	slices.SortFunc(pub.Symbols, func(a, b indexer.PublishedSymbol) int {
		return cmp.Compare(indexer.SymbolID(a.Identity), indexer.SymbolID(b.Identity))
	})
	indexed, err := indexer.Publish(ctx, database, pub.Publication)
	return pub, indexed.SnapshotID, err
}

func (pub *sqlPublication) addSource(source mappedSource) error {
	analysis, err := sqlsource.AnalyzeMigration(source.Content, source.Dialect, source.DefaultSchema)
	if err != nil {
		return fmt.Errorf("%s: %w", source.Filename, err)
	}
	pub.files[source.Filename] = source
	position := sourcePositions(source.Content)
	doc := storage.DocumentContent{Version: storage.DocumentFormatVersion, PackagePath: source.Package,
		Symbols: []storage.DocumentSymbol{}, Occurrences: []storage.DocumentOccurrence{}, Diagnostics: []storage.DocumentDiagnostic{}}
	name := strings.TrimSuffix(path.Base(source.Filename), path.Ext(source.Filename))
	if metadataName, ok := source.Metadata["name"].(string); ok && metadataName != "" {
		name = metadataName
	}
	var script string
	if source.Content != "" {
		script = pub.declare(&doc, declarationFact{
			name: name, kind: "sql.script", discriminator: source.Filename, props: source.Metadata,
			span: position.rangeOf(sqlglot.SourceRange{End: len(position.offsets) - 1}),
		}, source.Content)
	}
	for _, operation := range analysis.Accesses {
		pub.addAccess(&doc, source, position, operation, script)
	}
	for idx, feature := range analysis.Features {
		pub.declare(&doc, declarationFact{
			name: feature.Name, kind: "sql.feature", discriminator: source.Filename + ":" + strconv.Itoa(idx),
			props: map[string]any{"min_compatibility_level": feature.MinimumLevel},
			span:  position.rangeOf(sqlglot.SourceRange{Start: feature.Start, End: feature.Start + len(feature.Name)}),
		}, source.Content)
	}
	for _, issue := range analysis.Issues {
		span := position.rangeOf(issue.Range)
		file := filepath.Join(pub.Location.URI, filepath.FromSlash(source.reportPath()))
		finding := models.NewViolationBuilder().WithFile(file).WithMessage(issue.Message).WithSource("uir").WithRuleFromLinter("uir", issue.Rule).Build()
		finding.Line, finding.Column, finding.Severity = span.Range[0], span.Range[1], models.SeverityError
		pub.findings = append(pub.findings, finding)
	}
	slices.SortStableFunc(doc.Occurrences, func(a, b storage.DocumentOccurrence) int { return cmp.Compare(a.Bytes[0], b.Bytes[0]) })
	pub.Documents = append(pub.Documents, indexer.PublishedDocument{
		PathKey: source.Filename, PackagePath: source.Package, Source: source.Content, ContentHash: sqlDigest(source.Content), Content: doc,
	})
	return nil
}

type declarationFact struct {
	name, kind, discriminator string
	props                     map[string]any
	span                      sqlSpan
}

func (pub *sqlPublication) declare(doc *storage.DocumentContent, fact declarationFact, content string) string {
	identity := indexer.Identity{ModuleKey: pub.RootKey, PackagePath: doc.PackagePath, Kind: fact.kind, Name: fact.name, ParameterTypes: []string{fact.discriminator}}
	id := pub.addIdentity(identity)
	identifier := uir.Identifier{Module: pub.RootKey, Package: doc.PackagePath, Method: fact.name, Signature: fact.discriminator, NodeType: uir.NodeTypeFunction}
	shape := fact.kind + " " + fact.name
	doc.Symbols = append(doc.Symbols, storage.DocumentSymbol{
		ID: &id, Key: identifier.IdentityKey(), Kind: fact.kind, Visibility: "exported", Identifier: identifier,
		Shape: shape, PublicShape: shape, PublicHash: sqlDigest(shape), PrivateHash: sqlDigest(content), ContentHash: sqlDigest(content),
		Name: fact.span.Range, NameBytes: fact.span.Bytes, Extent: fact.span.Range, ExtentBytes: fact.span.Bytes, Props: fact.props,
	})
	return id
}

func (pub *sqlPublication) addAccess(doc *storage.DocumentContent, source mappedSource, position sqlPosition, operation access.Access, script string) {
	name, schema, kind := operation.Object.Name, operation.Object.Schema, operation.ObjectKind
	if source.Dialect == "tsql" || source.Dialect == "sqlite" {
		name, schema = strings.ToLower(name), strings.ToLower(schema)
	}
	if kind == "function" {
		kind = "sql.function"
	}
	module := pub.RootKey
	if operation.Object.Database != "" {
		module += "/" + operation.Object.Database
	}
	id := pub.addIdentity(indexer.Identity{ModuleKey: module, PackagePath: schema, Kind: kind, Name: name})
	span := position.rangeOf(*operation.Range)
	witness := storage.DocumentOccurrence{Symbol: &id, Enclosing: &script, Range: span.Range, Bytes: span.Bytes}
	switch operation.Kind {
	case access.Read:
		witness.Role = "reference"
	case access.Write:
		witness.Role = "write"
	case access.Call:
		witness.Role = "call"
		witness.Target = &uir.Identifier{Module: module, Package: schema, Method: name, NodeType: uir.NodeTypeFunction}
		witness.StatementPath = strconv.Itoa(len(doc.Occurrences))
	}
	doc.Occurrences = append(doc.Occurrences, witness)
}

func (pub *sqlPublication) addIdentity(identity indexer.Identity) string {
	id := indexer.SymbolID(identity)
	pub.symbols[id] = indexer.PublishedSymbol{Identity: identity, Visibility: "exported"}
	return id
}

func (source mappedSource) reportPath() string {
	return cmp.Or(source.Origin, source.Filename)
}

func (source mappedSource) reportSource(root string, historical bool) linters.SourceFile {
	var inputs []string
	for _, input := range source.Inputs {
		inputs = append(inputs, filepath.Join(root, filepath.FromSlash(input)))
	}
	payload := linters.SourceFile{Inputs: inputs}
	if source.Origin == "" || historical {
		payload.Content = source.Content
	}
	return payload
}

type sqlSpan struct {
	Bytes storage.ByteSpan
	Range storage.Range
}

type sqlPosition struct {
	offsets []int
	lines   syntax.LineIndex
}

func sourcePositions(content string) sqlPosition {
	offsets := make([]int, 0, len(content)+1)
	for i := range content {
		offsets = append(offsets, i)
	}
	return sqlPosition{offsets: append(offsets, len(content)), lines: syntax.NewLineIndex([]byte(content))}
}

func (pos sqlPosition) rangeOf(span sqlglot.SourceRange) sqlSpan {
	end := len(pos.offsets) - 1
	first, last := min(max(span.Start, 0), end), min(max(span.End, span.Start, 0), end)
	bytes := storage.ByteSpan{pos.offsets[first], pos.offsets[last]}
	return sqlSpan{Bytes: bytes, Range: pos.lines.RangeOf(bytes)}
}

func sqlDigest(content string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
}
