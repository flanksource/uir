package symboldiff

import (
	"github.com/flanksource/clicky/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	saveBefore = "func (s *Store) Save(ctx context.Context, inv Invoice) error"
	saveAfter  = "func (s *Store) Save(ctx context.Context, inv Invoice, opts ...SaveOption) (Receipt, error)"
	savePrefix = "func (s *Store) Save(ctx context.Context, inv Invoice"

	invoiceBefore = "type Invoice struct {\n\tID    InvoiceID\n\tTotal int\n}"
	invoiceAfter  = "type Invoice struct {\n\tID    InvoiceID\n\tTotal Money\n\tNotes string\n}"
)

func tokens(pairs ...string) []Token {
	result := make([]Token, 0, len(pairs)/2)
	for index := 0; index < len(pairs); index += 2 {
		result = append(result, Token{Op: Op(pairs[index]), Text: pairs[index+1]})
	}
	return result
}

var _ = Describe("shape diff", func() {
	It("diffs a one-line signature as one replaced pair with token marks", func() {
		diff := DiffShapes(saveBefore, saveAfter)
		Expect(diff).To(Equal(ShapeDiff{
			{Op: OpDelete, Paired: true, Text: saveBefore, Tokens: tokens("equal", savePrefix, "delete", ") error")},
			{Op: OpInsert, Paired: true, Text: saveAfter, Tokens: tokens("equal", savePrefix, "insert", ", opts ...SaveOption) (Receipt, error)")},
		}))
		Expect(diff.String()).To(Equal("- " + saveBefore + "\n+ " + saveAfter + "\n"))
		Expect(diff.MarkdownWithOptions(api.MarkdownOptions{NoColor: true})).To(Equal(
			"~ func (s \\*Store) Save(ctx context.Context, inv Invoice~~) error~~**, opts ...SaveOption) (Receipt, error)**\n"))
		Expect(diff.Markdown()).To(And(ContainSubstring("~~) error~~"), ContainSubstring("**, opts ...SaveOption) (Receipt, error)**")))
		ansi := diff.ANSI()
		Expect(ansi).To(HavePrefix("~ " + savePrefix))
		Expect(ansi).To(MatchRegexp(`\x1b\[9m\x1b\[[0-9;]+m\) error`), "the removed tail is struck through and coloured")
		Expect(ansi).To(MatchRegexp(`\x1b\[1;[0-9;]*m, opts \.\.\.SaveOption\) \(Receipt, error\)`), "the added tail is bold and coloured")
		Expect(diff.HTML()).To(And(ContainSubstring("<s>) error</s>"), ContainSubstring("<strong>, opts ...SaveOption) (Receipt, error)</strong>")))
	})

	It("diffs a struct field change as a field-level line diff", func() {
		diff := DiffShapes(invoiceBefore, invoiceAfter)
		Expect(diff).To(Equal(ShapeDiff{
			{Op: OpEqual, Text: "type Invoice struct {", Tokens: tokens("equal", "type Invoice struct {")},
			{Op: OpEqual, Text: "\tID    InvoiceID", Tokens: tokens("equal", "\tID    InvoiceID")},
			{Op: OpDelete, Paired: true, Text: "\tTotal int", Tokens: tokens("equal", "\tTotal ", "delete", "int")},
			{Op: OpInsert, Paired: true, Text: "\tTotal Money", Tokens: tokens("equal", "\tTotal ", "insert", "Money")},
			{Op: OpInsert, Text: "\tNotes string", Tokens: tokens("insert", "\tNotes string")},
			{Op: OpEqual, Text: "}", Tokens: tokens("equal", "}")},
		}))
		Expect(diff.String()).To(Equal("  type Invoice struct {\n  \tID    InvoiceID\n- \tTotal int\n+ \tTotal Money\n+ \tNotes string\n  }\n"))
		Expect(diff.MarkdownWithOptions(api.MarkdownOptions{NoColor: true})).To(Equal(
			"  type Invoice struct {\n  \tID    InvoiceID\n~ \tTotal ~~int~~**Money**\n+ \t**Notes string**\n  }\n"))
		Expect(diff.ANSI()).To(And(MatchRegexp(`\x1b\[9m\x1b\[[0-9;]+mint`), MatchRegexp(`\x1b\[1;[0-9;]*mMoney`), MatchRegexp(`\+ \t\x1b\[1;[0-9;]*mNotes string`)))
	})

	It("keeps paired and unpaired lines of one hunk in the order they appear", func() {
		diff := DiffShapes("type T struct {\n\tA int\n\tB string\n}", "type T struct {\n\tX float\n\tB Text\n}")
		Expect(diff.String()).To(Equal("  type T struct {\n- \tA int\n+ \tX float\n- \tB string\n+ \tB Text\n  }\n"))
		Expect(diff.MarkdownWithOptions(api.MarkdownOptions{NoColor: true})).To(Equal(
			"  type T struct {\n- \t~~A int~~\n+ \t**X float**\n~ \tB ~~string~~**Text**\n  }\n"))
	})

	It("escapes markdown metacharacters of token text so they render literally", func() {
		diff := DiffShapes("func Pick(m map[string]*_Item) `tag`", "func Pick(m map[string]*_Entry) `tag`")
		Expect(diff.MarkdownWithOptions(api.MarkdownOptions{NoColor: true})).To(Equal(
			"~ func Pick(m map\\[string\\]\\*~~\\_Item~~**\\_Entry**) \\`tag\\`\n"))
		Expect(diff.String()).To(Equal("- func Pick(m map[string]*_Item) `tag`\n+ func Pick(m map[string]*_Entry) `tag`\n"))
	})

	It("diffs a parameter-type change as one signature edit across the removed and added identities", func() {
		diff := DiffShapes("func Half(a Amount) Amount", "func Half(a *Amount) Amount")
		Expect(diff).To(Equal(ShapeDiff{
			{Op: OpDelete, Paired: true, Text: "func Half(a Amount) Amount", Tokens: tokens("equal", "func Half(a Amount) Amount")},
			{Op: OpInsert, Paired: true, Text: "func Half(a *Amount) Amount", Tokens: tokens("equal", "func Half(a ", "insert", "*", "equal", "Amount) Amount")},
		}))
		Expect(diff.String()).To(Equal("- func Half(a Amount) Amount\n+ func Half(a *Amount) Amount\n"))
		Expect(diff.MarkdownWithOptions(api.MarkdownOptions{NoColor: true})).To(Equal("~ func Half(a **\\***Amount) Amount\n"))
		Expect(diff.ANSI()).To(MatchRegexp(`func Half\(a \x1b\[1;[0-9;]*m\*\x1b\[0mAmount\) Amount`))
	})

	It("treats a whitespace-only realignment as equal and keeps the new layout", func() {
		diff := DiffShapes("type T struct {\n\tA int\n}", "type T struct {\n\tA    int\n\tLong string\n}")
		Expect(diff).To(Equal(ShapeDiff{
			{Op: OpEqual, Text: "type T struct {", Tokens: tokens("equal", "type T struct {")},
			{Op: OpEqual, Text: "\tA    int", Tokens: tokens("equal", "\tA    int")},
			{Op: OpInsert, Text: "\tLong string", Tokens: tokens("insert", "\tLong string")},
			{Op: OpEqual, Text: "}", Tokens: tokens("equal", "}")},
		}))
	})

	It("diffs equal shapes to all-equal lines", func() {
		Expect(DiffShapes("func A()", "func A()")).To(Equal(ShapeDiff{{Op: OpEqual, Text: "func A()", Tokens: tokens("equal", "func A()")}}))
	})
})
