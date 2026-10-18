package indexer

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/indexer/syntax"
	"github.com/flanksource/uir/storage"
	"github.com/flanksource/uir/storage/symbolhandle"
)

// nodeContentNoneVersion is digested into the content_hash of a symbol with no implementation anywhere
// beneath it (uir.Fingerprint's Content is ""), so every symbol has a 64-hex content hash and two
// bodiless declarations compare equal.
const nodeContentNoneVersion = "uir-node-content-none-v1"

// nodeScope is the module and package every symbol of one document is declared in.
type nodeScope struct {
	moduleKey, packagePath string
	declared               declaredNodes
}

// nodeDocument walks a document's nodes into symbols, generates its source, and returns the published
// document and its symbols, owners first.
func nodeDocument(moduleKey string, document NodeDocument, declared declaredNodes) (PublishedDocument, []PublishedSymbol, error) {
	scope := nodeScope{moduleKey: moduleKey, packagePath: document.PackagePath, declared: declared}
	ordinals := map[string]int{}
	var writer sourceWriter
	var published []PublishedSymbol
	for i, node := range document.Nodes {
		symbol, err := scope.symbol(nodeValue(node), nil, "", false)
		if err != nil {
			return PublishedDocument{}, nil, err
		}
		symbol.slot = topLevelSlots[symbol.identity.Kind]
		symbol.ordinal = ordinals[symbol.slot]
		ordinals[symbol.slot]++
		if i > 0 {
			writer.text.WriteString("\n")
		}
		if err := writer.write(symbol, ""); err != nil {
			return PublishedDocument{}, nil, err
		}
		published = append(published, symbol.published()...)
	}
	writer.text.WriteString("\n")
	content, err := writer.content(document.PackagePath)
	if err != nil {
		return PublishedDocument{}, nil, err
	}
	source := writer.text.String()
	return PublishedDocument{
		PathKey: document.PathKey, PackagePath: document.PackagePath, ContentHash: hashBytes([]byte(source)), Source: source, Content: content,
	}, published, nil
}

// symbol is node as a symbol owned by owner (nil at top level) in child slot slot, with its members.
func (scope nodeScope) symbol(node uir.Node, owner *nodeSymbol, slot string, private bool) (nodeSymbol, error) {
	kind, err := symbolKind(node, owner != nil)
	if err != nil {
		return nodeSymbol{}, err
	}
	name := node.GetIdentifier().GetName()
	if name == "" {
		return nodeSymbol{}, fmt.Errorf("a %s node has no name", uir.ShapeKind(node))
	}
	parameters, err := nodeParameterTypes(node)
	if err != nil {
		return nodeSymbol{}, err
	}
	symbol := nodeSymbol{
		node: node, slot: slot, visibility: "exported",
		identity: Identity{ModuleKey: scope.moduleKey, PackagePath: scope.packagePath, Kind: kind, Name: name, ParameterTypes: parameters},
	}
	if private || !uir.IsPublic(node) || (owner != nil && owner.visibility == "internal") {
		symbol.visibility = "internal"
	}
	if owner != nil {
		symbol.identity.OwnerID, symbol.parentKey = SymbolID(owner.identity), owner.node.GetIdentifier().IdentityKey()
	}
	if err := scope.declare(symbol, owner); err != nil {
		return nodeSymbol{}, err
	}
	for _, group := range memberSlots(node) {
		for i, member := range group.nodes {
			child, err := scope.symbol(member, &symbol, group.name, group.private)
			if err != nil {
				return nodeSymbol{}, err
			}
			child.ordinal = i
			symbol.members = append(symbol.members, child)
		}
	}
	return symbol, nil
}

// declare records the symbol's id and identifier key; a second declaration of either is an error.
func (scope nodeScope) declare(symbol nodeSymbol, owner *nodeSymbol) error {
	id, key := SymbolID(symbol.identity), symbol.node.GetIdentifier().IdentityKey()
	switch {
	case scope.declared.ids[id]:
		return fmt.Errorf("%s is declared twice", symbol.subject(owner))
	case scope.declared.keys[key]:
		return fmt.Errorf("%s has identifier key %s, which another symbol already has; qualify members with their owner, as uir.NewTable and uir.NewType do", symbol.subject(owner), key)
	}
	scope.declared.ids[id], scope.declared.keys[key] = true, true
	return nil
}

// published is the symbol and its members, owners first.
func (symbol nodeSymbol) published() []PublishedSymbol {
	published := []PublishedSymbol{{Identity: symbol.identity, Visibility: symbol.visibility}}
	for _, member := range symbol.members {
		published = append(published, member.published()...)
	}
	return published
}

// sourceWriter generates a document's source: per top-level symbol its private shape text with each
// symbol's name after its kind, each member's line expanded in place to the member's own text, and the
// symbol's lowered body rendered under it, so a symbol's extent is its text and nests inside its owner's.
type sourceWriter struct {
	text    strings.Builder
	written []writtenSymbol
}

type writtenSymbol struct {
	symbol       nodeSymbol
	name, extent storage.ByteSpan
}

func (writer *sourceWriter) write(symbol nodeSymbol, indent string) error {
	lines := strings.Split(uir.Shape(symbol.node, uir.LayerPrivate), "\n")
	head, err := namedLine(symbol.node, lines[0])
	if err != nil {
		return err
	}
	var inner, closing []string
	if len(lines) > 1 {
		inner, closing = lines[1:len(lines)-1], lines[len(lines)-1:]
	}
	index := len(writer.written)
	writer.written = append(writer.written, writtenSymbol{symbol: symbol})
	writer.text.WriteString(indent)
	start := writer.text.Len()
	nameStart := start + len(uir.ShapeKind(symbol.node)) + 1
	writer.text.WriteString(head)
	if err := writer.writeMembers(symbol, inner, indent); err != nil {
		return err
	}
	writer.writeBody(nodeBody(symbol.node), indent+"  ")
	for _, line := range closing {
		writer.text.WriteString("\n" + indent + line)
	}
	writer.written[index].name = storage.ByteSpan{nameStart, nameStart + len(symbol.identity.Name)}
	writer.written[index].extent = storage.ByteSpan{start, writer.text.Len()}
	return nil
}

// writeMembers writes the lines inside the symbol's shape braces, expanding the line uir.MemberLine
// gives each member to the member's own text. A member whose line the shape does not hold is an error.
func (writer *sourceWriter) writeMembers(symbol nodeSymbol, shapeLines []string, indent string) error {
	members := make(map[string]nodeSymbol, len(symbol.members))
	for _, member := range symbol.members {
		line := uir.MemberLine(member.node, uir.LayerPrivate)
		if _, taken := members[line]; taken {
			return fmt.Errorf("members %s share the shape line %q", member.identity.Name, line)
		}
		members[line] = member
	}
	for _, line := range shapeLines {
		writer.text.WriteString("\n")
		memberLine := strings.TrimPrefix(line, "  ")
		member, isSymbol := members[memberLine]
		if !isSymbol {
			writer.text.WriteString(indent + line)
			continue
		}
		delete(members, memberLine)
		if err := writer.write(member, indent+"  "); err != nil {
			return err
		}
	}
	if len(members) > 0 {
		return fmt.Errorf("member lines %q are not in the private shape of %s", slices.Sorted(maps.Keys(members)), symbol.identity.Name)
	}
	return nil
}

// writeBody writes a lowered body as the plain text of its Pretty rendering, one line per line at
// indent; a blank line stays blank.
func (writer *sourceWriter) writeBody(body *uir.BlockStmt, indent string) {
	if body == nil {
		return
	}
	for _, line := range strings.Split(body.Pretty().String(), "\n") {
		writer.text.WriteString("\n")
		if line != "" {
			writer.text.WriteString(indent + line)
		}
	}
}

// namedLine inserts the node's member name after the kind that opens the first line of its shape.
func namedLine(node uir.Node, line string) (string, error) {
	kind := string(uir.ShapeKind(node))
	rest, found := strings.CutPrefix(line, kind)
	if !found {
		return "", fmt.Errorf("shape %q of %s does not open with its kind %q", line, uir.MemberName(node), kind)
	}
	return kind + " " + uir.MemberName(node) + rest, nil
}

// content is the typed document of everything written, symbols in extent order.
func (writer *sourceWriter) content(packagePath string) (storage.DocumentContent, error) {
	lines := syntax.NewLineIndex([]byte(writer.text.String()))
	content := storage.DocumentContent{
		Version: storage.DocumentFormatVersion, PackagePath: packagePath, Symbols: make([]storage.DocumentSymbol, len(writer.written)),
		Occurrences: []storage.DocumentOccurrence{}, Diagnostics: []storage.DocumentDiagnostic{},
	}
	for i, written := range writer.written {
		symbol, err := written.documentSymbol(lines)
		if err != nil {
			return storage.DocumentContent{}, err
		}
		content.Symbols[i] = symbol
	}
	return content, nil
}

func (written writtenSymbol) documentSymbol(lines syntax.LineIndex) (storage.DocumentSymbol, error) {
	node, symbol := written.symbol.node, written.symbol
	prints := uir.Fingerprint(node)
	if prints.Content == "" {
		prints.Content = hashBytes([]byte(nodeContentNoneVersion))
	}
	for _, hash := range []string{prints.Public, prints.Private, prints.Content} {
		if !sha256Hex.MatchString(hash) {
			return storage.DocumentSymbol{}, fmt.Errorf("%s: layer hash %q is not a 64-character hex SHA-256", symbol.identity.Name, hash)
		}
	}
	payload, err := uir.MarshalNode(node)
	if err != nil {
		return storage.DocumentSymbol{}, fmt.Errorf("encode %s: %w", symbol.identity.Name, err)
	}
	id := SymbolID(symbol.identity)
	declared := storage.DocumentSymbol{
		ID: &id, Key: node.GetIdentifier().IdentityKey(), Kind: symbol.identity.Kind, Visibility: symbol.visibility,
		Shape: uir.Shape(node, uir.LayerPrivate), PublicShape: uir.Shape(node, uir.LayerPublic),
		PublicHash: prints.Public, PrivateHash: prints.Private, ContentHash: prints.Content,
		Name: lines.RangeOf(written.name), NameBytes: written.name, Extent: lines.RangeOf(written.extent), ExtentBytes: written.extent,
		Identifier: node.GetIdentifier(), ParentKey: symbol.parentKey, ChildSlot: symbol.slot, Ordinal: symbol.ordinal,
		Payload: storage.JSON(payload), SemanticHash: node.Hash(),
	}
	if declared.Kind == symbolhandle.KindType.String() {
		if declared.TypeForm, err = storage.ClassifyTypeShape(declared); err != nil {
			return storage.DocumentSymbol{}, err
		}
	}
	return declared, nil
}
