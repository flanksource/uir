// Package sqlsyntax parses the UIR SQL navigation language.
package sqlsyntax

import "github.com/alecthomas/participle/v2/lexer"

type Query struct {
	Pos        lexer.Position
	Projection *Projection `parser:"'SELECT' @@"`
	Source     *Source     `parser:"'FROM' @@"`
	Alias      string      `parser:"('AS' @Ident)?"`
	Where      *Predicate  `parser:"('WHERE' @@)?"`
	Order      []*Ordering `parser:"('ORDER' 'BY' @@ (',' @@)*)?"`
	Limit      *int        `parser:"('LIMIT' @Number)?"`
	Semicolon  bool        `parser:"@';'?"`
}

type Source struct {
	Pos       lexer.Position
	EndPos    lexer.Position
	Selector  string    `parser:"( @Selector"`
	Name      string    `parser:"| @Ident '('"`
	Arguments []*Source `parser:"@@ (',' @@)*"`
	Options   []*Option `parser:"(',' @@)* ')' )"`
}

type Option struct {
	Pos   lexer.Position
	Name  string   `parser:"@Ident '=>'"`
	Value *Literal `parser:"@@"`
}

type Projection struct {
	Wildcard bool      `parser:"@'*' |"`
	Columns  []*Column `parser:"@@ (',' @@)*"`
}

type Column struct {
	Pos   lexer.Position
	Field string `parser:"@Ident"`
	Alias string `parser:"('AS' @Ident)?"`
}

type Ordering struct {
	Pos       lexer.Position
	Field     string `parser:"@Ident"`
	Direction string `parser:"@('ASC' | 'DESC')?"`
}

type Predicate struct {
	Terms []*Conjunction `parser:"@@ ('OR' @@)*"`
}

type Conjunction struct {
	Terms []*Condition `parser:"@@ ('AND' @@)*"`
}

type Condition struct {
	Pos        lexer.Position
	Not        *Condition  `parser:"'NOT' @@ |"`
	Group      *Predicate  `parser:"'(' @@ ')' |"`
	Comparison *Comparison `parser:"@@"`
}

type Comparison struct {
	Pos     lexer.Position
	Field   string      `parser:"@Ident"`
	Compare *Compare    `parser:"( @@ |"`
	Member  *Membership `parser:"@@ |"`
	Match   *Match      `parser:"@@ |"`
	Null    *NullCheck  `parser:"@@ )"`
}

type Compare struct {
	Operator string   `parser:"@('=' | '!=' | '<' | '<=' | '>' | '>=')"`
	Value    *Literal `parser:"@@"`
}

type Membership struct {
	Not      bool   `parser:"@'NOT'? 'IN'"`
	Selector string `parser:"@Selector"`
}

type Match struct {
	Not      bool      `parser:"@'NOT'?"`
	Operator string    `parser:"@('MATCH' | 'IMATCH')"`
	Pattern  SQLString `parser:"@String"`
}

type NullCheck struct {
	Not bool `parser:"'IS' @'NOT'? 'NULL'"`
}

type Literal struct {
	String  *SQLString  `parser:"@String |"`
	Number  *float64    `parser:"@Number |"`
	Boolean *SQLBoolean `parser:"@('TRUE' | 'FALSE') |"`
	Null    bool        `parser:"@'NULL'"`
}
