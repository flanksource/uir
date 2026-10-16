package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/flanksource/uir"
	"github.com/flanksource/uir/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The schema fixture is one database schema published from UIR nodes: two tables that both have an id
// column, a foreign key, an index and a trigger on orders, a function with SQL-typed parameters, and a
// view whose lowered definition is its content. schemaV2 adds a nullable column and an index to
// customers and rewrites the view's definition without changing its columns.
const (
	schemaRoot    = "example.org/warehouse"
	schemaPackage = "public"
	schemaURI     = "test://db/warehouse"
	tablesPath    = "public/tables.sql"
	viewsPath     = "public/views.sql"
)

func schemaColumn(name, sqlType string, fieldType uir.RecordFieldType, nullable bool) uir.RecordColumn {
	column := uir.NewColumn(name, sqlType, fieldType)
	column.Nullable = nullable
	return column
}

func idColumn() uir.RecordColumn {
	id := schemaColumn("id", "bigint", uir.RecordFieldTypeInt, false)
	id.PrimaryKey, id.AutoIncrement = true, true
	return id
}

func blockCalling(method string) uir.BlockStmt {
	return uir.NewBlock().WithStatements(uir.NewMethodCall(method).Build()).Build()
}

func customersTable(v2 bool) uir.RecordTable {
	table := uir.NewTable("customers", uir.Identifier{Package: schemaPackage}).
		WithColumns(idColumn(), schemaColumn("name", "text", uir.RecordFieldTypeString, false))
	if v2 {
		table.WithColumns(schemaColumn("email", "text", uir.RecordFieldTypeString, true)).
			WithIndexes(uir.NewIndex("ix_customers_name", "name"))
	}
	return table.Build()
}

func ordersTable(auditCall string) uir.RecordTable {
	audit := uir.NewMethod("orders_audit").WithBody(blockCalling(auditCall)).Build()
	audit.Properties = map[string]any{uir.TriggerEventProperty: "insert", uir.TriggerTimingProperty: "after"}
	return uir.NewTable("orders", uir.Identifier{Package: schemaPackage}).
		WithColumns(idColumn(), schemaColumn("customer_id", "bigint", uir.RecordFieldTypeInt, false)).
		WithIndexes(uir.NewIndex("ix_orders_customer", "customer_id")).
		WithForeignKeys(uir.NewForeignKey("fk_orders_customer", []string{"customer_id"}, "customers", []string{"id"})).
		WithTriggers(audit).Build()
}

func chargeFunction() uir.MethodNode {
	amount := uir.Field("amount", uir.RecordFieldTypeFloat)
	amount.SQLType = "numeric"
	return uir.NewMethod("charge", uir.Identifier{Package: schemaPackage, NodeType: uir.NodeTypeFunction}).
		WithParam(amount).WithBody(blockCalling("apply_charge")).Build()
}

func totalsView(definition string) uir.RecordTable {
	view := uir.NewTable("order_totals", uir.Identifier{Package: schemaPackage}).
		WithColumns(schemaColumn("customer_id", "bigint", uir.RecordFieldTypeInt, false), schemaColumn("total", "numeric", uir.RecordFieldTypeFloat, true)).
		Build()
	view.RecordType = uir.RecordTypeView
	block := blockCalling(definition)
	view.Definition = &block
	return view
}

func schemaNodes(revision string) NodePublication {
	v2 := revision == "schema-v2"
	definition := "sum_orders"
	if v2 {
		definition = "sum_orders_by_customer"
	}
	return NodePublication{
		RootKey: schemaRoot, Name: "warehouse", Location: ExternalLocation{URI: schemaURI}, Revision: revision,
		Documents: []NodeDocument{
			{PathKey: tablesPath, PackagePath: schemaPackage, Nodes: []uir.Node{customersTable(v2), ordersTable("audit_log"), chargeFunction()}},
			{PathKey: viewsPath, PackagePath: schemaPackage, Nodes: []uir.Node{totalsView(definition)}},
		},
	}
}

func schemaPublication(revision string) Publication {
	GinkgoHelper()
	publication, err := PublicationFromNodes(schemaNodes(revision))
	Expect(err).ToNot(HaveOccurred())
	return publication
}

// publishedRows is each published symbol as "<kind> <owner>.<name> <visibility>".
func publishedRows(publication Publication) []string {
	names := map[string]string{}
	rows := make([]string, len(publication.Symbols))
	for i, symbol := range publication.Symbols {
		names[SymbolID(symbol.Identity)] = symbol.Name
		qualified := symbol.Name
		if symbol.OwnerID != "" {
			qualified = names[symbol.OwnerID] + "." + symbol.Name
		}
		rows[i] = symbol.Kind + " " + qualified + " " + symbol.Visibility
	}
	return rows
}

func declaredSymbol(publication Publication, pathKey, key string) storage.DocumentSymbol {
	GinkgoHelper()
	for _, document := range publication.Documents {
		for _, symbol := range document.Content.Symbols {
			if document.PathKey == pathKey && symbol.Key == key {
				return symbol
			}
		}
	}
	Fail("no symbol " + key + " in " + pathKey)
	return storage.DocumentSymbol{}
}

// The ledger fixture is one class published from UIR nodes, as a non-Go producer lowers it: a private
// and a public field, a constructor, and a method whose lowered body calls postCall.
const (
	ledgerPackage = "billing"
	ledgerPath    = "billing/Ledger.cls"
)

func ledgerField(name string, visibility uir.Visibility) uir.RecordField {
	field := uir.Field(name, uir.RecordFieldTypeString)
	field.Visibility = visibility
	return field
}

func ledgerClass(postCall string) uir.TypedNode {
	amount := uir.Field("amount", uir.RecordFieldTypeFloat)
	constructor := uir.NewMethod("Ledger", uir.Identifier{NodeType: uir.NodeTypeConstructor}).
		WithVisibility(uir.VisibilityPublic).WithParam(ledgerField("owner", uir.VisibilityNA)).Build()
	post := uir.NewMethod("post").WithVisibility(uir.VisibilityPublic).
		WithParam(amount).WithBody(blockCalling(postCall)).Build()
	return uir.NewType("Ledger").WithPackage(ledgerPackage).WithVisibility(uir.VisibilityPublic).
		WithVariables(ledgerField("balance", uir.VisibilityPrivate), ledgerField("owner", uir.VisibilityPublic)).
		WithConstructor(constructor).WithMethod(post).Build()
}

// nodeDocumentOf publishes nodes as the one document at pathKey and returns it.
func nodeDocumentOf(pathKey, packagePath string, nodes ...uir.Node) PublishedDocument {
	GinkgoHelper()
	publication, err := PublicationFromNodes(NodePublication{
		RootKey: schemaRoot, Name: "warehouse", Location: ExternalLocation{URI: schemaURI}, Revision: "r1",
		Documents: []NodeDocument{{PathKey: pathKey, PackagePath: packagePath, Nodes: nodes}},
	})
	Expect(err).ToNot(HaveOccurred())
	return publication.Documents[0]
}

// changedLines are the byte spans, newline excluded, of the lines of after that differ from before,
// which has as many lines.
func changedLines(before, after string) []storage.ByteSpan {
	GinkgoHelper()
	beforeLines, afterLines := strings.SplitAfter(before, "\n"), strings.SplitAfter(after, "\n")
	Expect(afterLines).To(HaveLen(len(beforeLines)))
	var spans []storage.ByteSpan
	offset := 0
	for i, line := range afterLines {
		if line != beforeLines[i] {
			spans = append(spans, storage.ByteSpan{offset, offset + len(strings.TrimSuffix(line, "\n"))})
		}
		offset += len(line)
	}
	return spans
}

var _ = Describe("PublicationFromNodes", func() {
	It("publishes every table, member, function, and view with its owner and visibility", func() {
		Expect(publishedRows(schemaPublication("schema-v1"))).To(Equal([]string{
			"table customers exported",
			"column customers.id exported",
			"column customers.name exported",
			"table orders exported",
			"column orders.id exported",
			"column orders.customer_id exported",
			"index orders.ix_orders_customer internal",
			"foreign_key orders.fk_orders_customer internal",
			"method orders.orders_audit internal",
			"func charge exported",
			"table order_totals exported",
			"column order_totals.customer_id exported",
			"column order_totals.total exported",
		}))
	})

	It("gives the id column of each table its own symbol id, and overloads their parameter types", func() {
		publication := schemaPublication("schema-v1")
		var ids []string
		for _, symbol := range publication.Symbols {
			if symbol.Name == "id" {
				ids = append(ids, SymbolID(symbol.Identity))
			}
			if symbol.Name == "charge" {
				Expect(symbol.ParameterTypes).To(Equal([]string{"numeric"}))
			}
		}
		Expect(ids).To(HaveLen(2))
		Expect(ids[0]).ToNot(Equal(ids[1]))
	})

	It("carries uir.Fingerprint's layers and uir.Shape's text, and a fixed digest for no content", func() {
		publication := schemaPublication("schema-v1")
		none := sha256.Sum256([]byte("uir-node-content-none-v1"))
		for _, node := range []struct {
			path    string
			node    uir.Node
			content string
		}{
			{tablesPath, customersTable(false), hex.EncodeToString(none[:])},
			{tablesPath, ordersTable("audit_log"), uir.Fingerprint(ordersTable("audit_log")).Content},
			{viewsPath, totalsView("sum_orders"), uir.Fingerprint(totalsView("sum_orders")).Content},
		} {
			prints := uir.Fingerprint(node.node)
			symbol := declaredSymbol(publication, node.path, node.node.GetIdentifier().IdentityKey())
			Expect([]string{symbol.PublicHash, symbol.PrivateHash, symbol.ContentHash, symbol.PublicShape, symbol.Shape, symbol.SemanticHash}).To(Equal([]string{
				prints.Public, prints.Private, node.content, uir.Shape(node.node, uir.LayerPublic), uir.Shape(node.node, uir.LayerPrivate), node.node.Hash(),
			}))
		}
		Expect(uir.Fingerprint(ordersTable("audit_log")).Content).To(HaveLen(64), "the trigger body is the table's content")
	})

	It("generates the same source for the same nodes, naming every symbol where its name bytes point", func() {
		first, second := schemaPublication("schema-v1"), schemaPublication("schema-v1")
		Expect(first.Documents).To(Equal(second.Documents))
		Expect(first.Documents[1].Source).To(Equal("table order_totals sql:view {\n" +
			"  column customer_id bigint (int) not null\n" +
			"  column total numeric (float) null\n" +
			"  sum_orders()\n" +
			"}\n"))
		for _, document := range first.Documents {
			for _, symbol := range document.Content.Symbols {
				Expect(document.Source[symbol.NameBytes[0]:symbol.NameBytes[1]]).To(Equal(symbol.Identifier.GetName()), symbol.Key)
			}
		}
	})

	DescribeTable("renders each lowered body inside its symbol's extent, so editing it changes lines only there",
		func(pathKey, packagePath string, node func(call string) uir.Node, key string) {
			before := nodeDocumentOf(pathKey, packagePath, node("first_call"))
			after := nodeDocumentOf(pathKey, packagePath, node("second_call"))
			extent := declaredSymbol(Publication{Documents: []PublishedDocument{after}}, pathKey, key).ExtentBytes
			changed := changedLines(before.Source, after.Source)
			Expect(changed).ToNot(BeEmpty())
			for _, span := range changed {
				Expect(after.Source[span[0]:span[1]]).To(ContainSubstring("second_call()"))
				Expect(span[0] >= extent[0] && span[1] <= extent[1]).To(BeTrue(), "line %v lies outside extent %v", span, extent)
			}
		},
		Entry("a trigger body", tablesPath, schemaPackage, func(call string) uir.Node { return ordersTable(call) },
			ordersTable("").Triggers[0].GetIdentifier().IdentityKey()),
		Entry("a view definition", viewsPath, schemaPackage, func(call string) uir.Node { return totalsView(call) },
			totalsView("").GetIdentifier().IdentityKey()),
		Entry("a method body of a class", ledgerPath, ledgerPackage, func(call string) uir.Node { return ledgerClass(call) },
			ledgerClass("").Methods[0].GetIdentifier().IdentityKey()),
	)

	It("publishes a class with its fields, constructor, and methods, struct-like and with its bodies in its source", func() {
		ledger := ledgerClass("append_entry")
		publication, err := PublicationFromNodes(NodePublication{
			RootKey: schemaRoot, Name: "warehouse", Location: ExternalLocation{URI: schemaURI}, Revision: "r1",
			Documents: []NodeDocument{{PathKey: ledgerPath, PackagePath: ledgerPackage, Nodes: []uir.Node{ledger}}},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(publishedRows(publication)).To(Equal([]string{
			"type Ledger exported",
			"field Ledger.balance internal",
			"field Ledger.owner exported",
			"constructor Ledger.Ledger exported",
			"method Ledger.post exported",
		}))
		Expect(declaredSymbol(publication, ledgerPath, ledger.GetIdentifier().IdentityKey()).TypeForm).To(Equal("struct"))
		Expect(publication.Documents[0].Source).To(Equal("class Ledger public {\n" +
			"  record_field balance (string) private\n" +
			"  record_field owner (string) public\n" +
			"  constructor Ledger public (owner (string))\n" +
			"  method post public (amount (float))\n" +
			"    append_entry()\n" +
			"}\n"))
	})

	DescribeTable("refuses nodes it cannot publish",
		func(mutate func(*NodePublication), message string) {
			input := schemaNodes("schema-v1")
			mutate(&input)
			_, err := PublicationFromNodes(input)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("a table declared twice", func(input *NodePublication) {
			input.Documents[1].Nodes = append(input.Documents[1].Nodes, customersTable(false))
		}, `document "public/views.sql": table public.customers is declared twice`),
		Entry("a node without a name", func(input *NodePublication) {
			input.Documents[0].Nodes = append(input.Documents[0].Nodes, uir.NewTable("").Build())
		}, `document "public/tables.sql": a table node has no name`),
		Entry("a node type with no publishable kind", func(input *NodePublication) {
			input.Documents[0].Nodes = append(input.Documents[0].Nodes, uir.PackageNode{})
		}, `document "public/tables.sql": node type "package" has no symbol kind PublicationFromNodes publishes`),
	)
})
