package symboldiff

import "strings"

const ledgerRoot = "example.org/ledger"

func goSum(version string) string {
	return "example.org/dependency " + version + " h1:" + strings.Repeat("A", 43) + "=\n" +
		"example.org/dependency " + version + "/go.mod h1:" + strings.Repeat("B", 43) + "=\n"
}

// ledgerBefore is commit A of example.org/ledger.
var ledgerBefore = map[string]*string{
	"go.mod": text("module " + ledgerRoot + "\n\ngo 1.26\n"),
	"go.sum": text(goSum("v1.0.0")),
	"money/money.go": text(lines(
		"package money",
		"",
		"// Amount is a quantity of money.",
		"type Amount struct {",
		"\tCents    int64",
		"\tCurrency string",
		"}",
		"",
		"// Add sums two amounts.",
		"func Add(a Amount, b Amount) Amount {",
		"\treturn Amount{Cents: a.Cents + b.Cents, Currency: a.Currency}",
		"}",
		"",
		"func Scale(a Amount, factor int) Amount {",
		"\treturn Amount{Cents: a.Cents * int64(factor), Currency: a.Currency}",
		"}",
		"",
		"func Unit() Amount { return Amount{Cents: 1} }",
		"",
		"func Half(a Amount) Amount { return Amount{Cents: a.Cents / 2, Currency: a.Currency} }",
		"",
		"func round(value int64) int64 { return value }",
	)),
	"book/book.go": text(lines(
		"package book",
		"",
		"import \"example.org/ledger/money\"",
		"",
		"type Book struct {",
		"\tTitle string",
		"\tPrice money.Amount",
		"}",
		"",
		"func Total(books []Book) money.Amount {",
		"\ttotal := money.Amount{}",
		"\tfor _, book := range books {",
		"\t\ttotal = money.Add(total, book.Price)",
		"\t}",
		"\treturn total",
		"}",
		"",
		"func Count(books []Book) int { return len(books) }",
	)),
}

// ledgerAfter is commit B: one edit per row of the design's edit table, plus a dependency bump.
//   - a comment inside Add and one at file scope (layout-only body row, file-scope lines)
//   - a second call to money.Add from Total (body)
//   - Scale's parameter renamed (signature)
//   - Half's parameter type changed (removed plus added, grouped)
//   - Count moved to count.go (moved)
//   - Unit's return type and Amount.Currency's field type changed (signature), Code added
//   - round's body changed (an internal body row, hidden by --visibility exported)
//   - go.sum bumped (a manifest file with file-scope lines only)
var ledgerAfter = map[string]*string{
	"go.sum": text(goSum("v1.1.0")),
	"money/money.go": text(lines(
		"package money",
		"// Values are in cents.",
		"",
		"// Code is an ISO currency code.",
		"type Code string",
		"",
		"// Amount is a quantity of money.",
		"type Amount struct {",
		"\tCents    int64",
		"\tCurrency Code",
		"}",
		"",
		"// Add sums two amounts.",
		"func Add(a Amount, b Amount) Amount {",
		"\t// Currencies are assumed equal.",
		"\treturn Amount{Cents: a.Cents + b.Cents, Currency: a.Currency}",
		"}",
		"",
		"func Scale(a Amount, by int) Amount {",
		"\treturn Amount{Cents: a.Cents * int64(by), Currency: a.Currency}",
		"}",
		"",
		"func Unit() *Amount { return &Amount{Cents: 1} }",
		"",
		"func Half(a *Amount) Amount { return Amount{Cents: a.Cents / 2, Currency: a.Currency} }",
		"",
		"func round(value int64) int64 { return value + 0 }",
	)),
	"book/book.go": text(lines(
		"package book",
		"",
		"import \"example.org/ledger/money\"",
		"",
		"type Book struct {",
		"\tTitle string",
		"\tPrice money.Amount",
		"}",
		"",
		"func Total(books []Book) money.Amount {",
		"\ttotal := money.Amount{}",
		"\tfor _, book := range books {",
		"\t\ttotal = money.Add(total, money.Add(book.Price, money.Amount{}))",
		"\t}",
		"\treturn total",
		"}",
	)),
	"book/count.go": text(lines(
		"package book",
		"",
		"func Count(books []Book) int { return len(books) }",
	)),
}

// ledgerStatAll is the plain-text rendering of the ledger diff with --visibility all --stat, less its
// header line.
const ledgerStatAll = `example.org/ledger  +2 -2
  go.sum (modified) [manifest]  +2 -2
    (file scope)                   +2 -2
example.org/ledger/book  +3 -2
  book/book.go (modified)  +1 -2
    Total                body      +1 -1
    (file scope)                   +0 -1
  book/count.go (added)  +2 -0
    Count                moved     +0 -0  from book/book.go
    (file scope)                   +2 -0
example.org/ledger/money  +11 -6
  money/money.go (modified)  +11 -6
    Add                  body      +1 -0  (comments or layout only; body_hash unchanged)
    Amount               signature +1 -1
        type Amount struct {
        	Cents    int64
      - 	Currency string
      + 	Currency Code
        }
    Amount.Currency      signature
      - Currency string
      + Currency Code
    Code                 added     +2 -0  type Code string
    Half                 removed   +0 -1  func Half(a Amount) Amount
    Half                 added     +1 -0
      - func Half(a Amount) Amount
      + func Half(a *Amount) Amount
    Scale                signature +2 -2
      - func Scale(a Amount, factor int) Amount
      + func Scale(a Amount, by int) Amount
    Unit                 signature +1 -1
      - func Unit() Amount
      + func Unit() *Amount
    round                body      +1 -1
    (file scope)                   +2 -0
`

// ledgerExported is the default output: exported rows only, without line counts.
const ledgerExported = `example.org/ledger/book
  book/book.go (modified)
    Total                body
  book/count.go (added)
    Count                moved      from book/book.go
example.org/ledger/money
  money/money.go (modified)  (1 hidden row)
    Amount               signature
        type Amount struct {
        	Cents    int64
      - 	Currency string
      + 	Currency Code
        }
    Amount.Currency      signature
      - Currency string
      + Currency Code
    Code                 added      type Code string
    Half                 removed    func Half(a Amount) Amount
    Half                 added
      - func Half(a Amount) Amount
      + func Half(a *Amount) Amount
    Scale                signature
      - func Scale(a Amount, factor int) Amount
      + func Scale(a Amount, by int) Amount
    Unit                 signature
      - func Unit() Amount
      + func Unit() *Amount
`
