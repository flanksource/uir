package symboldiff

import (
	"context"
	"go/scanner"
	"go/token"
	"strings"

	"github.com/flanksource/clicky/api"
	"github.com/pkg/diff/edit"
	"github.com/pkg/diff/myers"
)

// Op is one edit operation of a line or token diff.
type Op string

const (
	OpEqual  Op = "equal"
	OpDelete Op = "delete"
	OpInsert Op = "insert"
)

// Token is a run of source text with one edit operation.
type Token struct {
	Op   Op     `json:"op"`
	Text string `json:"text"`
}

// ShapeLine is one line of a shape diff. Text is the line as it appears on its side (the new side for
// an equal line). Paired marks a replaced line: a delete immediately followed by its insert, whose
// Tokens hold the token diff between the two (equal and deleted tokens on the delete line, equal and
// inserted tokens on the insert line).
type ShapeLine struct {
	Op     Op      `json:"op"`
	Text   string  `json:"text"`
	Paired bool    `json:"paired,omitempty"`
	Tokens []Token `json:"tokens"`
}

// ShapeDiff is the line diff of two canonical shapes, with token diffs of replaced lines.
type ShapeDiff []ShapeLine

// DiffShapes line-diffs two canonical shape renderings (lines equal when they differ only in
// whitespace), then token-diffs each replaced line pair that shares at least one token.
func DiffShapes(before, after string) ShapeDiff {
	oldLines, newLines := strings.Split(before, "\n"), strings.Split(after, "\n")
	script := myers.Diff(context.Background(), linePair{before: oldLines, after: newLines})
	diff := ShapeDiff{}
	for index := 0; index < len(script.Ranges); index++ {
		current := script.Ranges[index]
		if current.IsEqual() {
			for offset := range current.HighB - current.LowB {
				text := newLines[current.LowB+offset]
				diff = append(diff, ShapeLine{Op: OpEqual, Text: text, Tokens: wholeLine(OpEqual, text)})
			}
			continue
		}
		var deleted, inserted []string
		for ; index < len(script.Ranges) && !script.Ranges[index].IsEqual(); index++ {
			changed := script.Ranges[index]
			deleted = append(deleted, oldLines[changed.LowA:changed.HighA]...)
			inserted = append(inserted, newLines[changed.LowB:changed.HighB]...)
		}
		index--
		diff = append(diff, replacedLines(deleted, inserted)...)
	}
	return diff
}

// replacedLines pairs the i-th deleted line with the i-th inserted line when they share a token.
// Lines keep their hunk position: each run of unpaired lines is emitted, deletions before
// insertions, ahead of the pair that follows it.
func replacedLines(deleted, inserted []string) ShapeDiff {
	var result, deletions, insertions ShapeDiff
	for index := range max(len(deleted), len(inserted)) {
		if index < len(deleted) && index < len(inserted) {
			if segments := tokenDiff(deleted[index], inserted[index]); sharesToken(segments) {
				result = append(append(append(result, deletions...), insertions...),
					ShapeLine{Op: OpDelete, Paired: true, Text: deleted[index], Tokens: filterTokens(segments, OpInsert)},
					ShapeLine{Op: OpInsert, Paired: true, Text: inserted[index], Tokens: filterTokens(segments, OpDelete)})
				deletions, insertions = nil, nil
				continue
			}
		}
		if index < len(deleted) {
			deletions = append(deletions, ShapeLine{Op: OpDelete, Text: deleted[index], Tokens: wholeLine(OpDelete, deleted[index])})
		}
		if index < len(inserted) {
			insertions = append(insertions, ShapeLine{Op: OpInsert, Text: inserted[index], Tokens: wholeLine(OpInsert, inserted[index])})
		}
	}
	return append(append(result, deletions...), insertions...)
}

func wholeLine(op Op, text string) []Token {
	if text == "" {
		return []Token{}
	}
	return []Token{{Op: op, Text: text}}
}

func sharesToken(segments []Token) bool {
	for _, segment := range segments {
		if segment.Op == OpEqual && strings.TrimSpace(segment.Text) != "" {
			return true
		}
	}
	return false
}

// filterTokens drops one operation's segments and merges the adjacent segments that remain.
func filterTokens(segments []Token, drop Op) []Token {
	result := []Token{}
	for _, segment := range segments {
		if segment.Op != drop {
			result = appendToken(result, segment.Op, segment.Text)
		}
	}
	return result
}

func appendToken(tokens []Token, op Op, text string) []Token {
	if text == "" {
		return tokens
	}
	if last := len(tokens) - 1; last >= 0 && tokens[last].Op == op {
		tokens[last].Text += text
		return tokens
	}
	return append(tokens, Token{Op: op, Text: text})
}

type linePair struct{ before, after []string }

func (pair linePair) LenA() int { return len(pair.before) }
func (pair linePair) LenB() int { return len(pair.after) }
func (pair linePair) Equal(a, b int) bool {
	return strings.Join(strings.Fields(pair.before[a]), " ") == strings.Join(strings.Fields(pair.after[b]), " ")
}

// scanned is one go/scanner token of a line with the whitespace that precedes it.
type scanned struct{ leading, text string }

func scanLine(line string) ([]scanned, string) {
	fileSet := token.NewFileSet()
	file := fileSet.AddFile("", fileSet.Base(), len(line))
	var tokens scanner.Scanner
	tokens.Init(file, []byte(line), func(token.Position, string) {}, scanner.ScanComments)
	var result []scanned
	end := 0
	for {
		position, kind, literal := tokens.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.SEMICOLON && literal == "\n" {
			continue
		}
		start := file.Offset(position)
		length := len(literal)
		if length == 0 {
			length = len(kind.String())
		}
		result = append(result, scanned{leading: line[end:start], text: line[start : start+length]})
		end = start + length
	}
	return result, line[end:]
}

type tokenPair struct{ before, after []scanned }

func (pair tokenPair) LenA() int           { return len(pair.before) }
func (pair tokenPair) LenB() int           { return len(pair.after) }
func (pair tokenPair) Equal(a, b int) bool { return pair.before[a].text == pair.after[b].text }

// tokenEdit is one token of the merged sequence: a is set unless it was inserted, b unless deleted.
type tokenEdit struct {
	op   Op
	a, b *scanned
}

// tokenDiff diffs two lines by go/scanner tokens, dissolves short equal runs between edits so a
// changed tail reads as one replacement, and renders the merged sequence with its whitespace.
func tokenDiff(before, after string) []Token {
	oldTokens, _ := scanLine(before)
	newTokens, trailing := scanLine(after)
	script := myers.Diff(context.Background(), tokenPair{before: oldTokens, after: newTokens})
	var edits []tokenEdit
	for _, current := range script.Ranges {
		switch current.Op() {
		case edit.Eq:
			for offset := range current.HighA - current.LowA {
				edits = append(edits, tokenEdit{op: OpEqual, a: &oldTokens[current.LowA+offset], b: &newTokens[current.LowB+offset]})
			}
		case edit.Del:
			for index := current.LowA; index < current.HighA; index++ {
				edits = append(edits, tokenEdit{op: OpDelete, a: &oldTokens[index]})
			}
		case edit.Ins:
			for index := current.LowB; index < current.HighB; index++ {
				edits = append(edits, tokenEdit{op: OpInsert, b: &newTokens[index]})
			}
		}
	}
	edits = orderRegions(dissolveEqualities(edits))
	var segments []Token
	previous := OpEqual
	for _, current := range edits {
		switch {
		case current.op == OpEqual:
			segments = appendToken(segments, OpEqual, current.b.leading+current.b.text)
		case current.op == previous:
			side := current.b
			if current.op == OpDelete {
				side = current.a
			}
			segments = appendToken(segments, current.op, side.leading+side.text)
		case current.op == OpDelete || previous == OpEqual:
			side := current.b
			if current.op == OpDelete {
				side = current.a
			}
			segments = appendToken(appendToken(segments, OpEqual, side.leading), current.op, side.text)
		default:
			segments = appendToken(segments, OpInsert, current.b.text)
		}
		previous = current.op
	}
	return appendToken(segments, OpEqual, trailing)
}

// dissolveEqualities turns an equal run that lies between two edit regions into a deletion plus an
// insertion when it is no longer than the larger side of the edits on each of its sides.
func dissolveEqualities(edits []tokenEdit) []tokenEdit {
	for changed := true; changed; {
		changed = false
		for start := 0; start < len(edits); {
			if edits[start].op != OpEqual {
				start++
				continue
			}
			end := start
			for end < len(edits) && edits[end].op == OpEqual {
				end++
			}
			length := end - start
			if start > 0 && end < len(edits) && length <= editWeight(edits, start-1, -1) && length <= editWeight(edits, end, 1) {
				var replaced []tokenEdit
				for _, equal := range edits[start:end] {
					replaced = append(replaced, tokenEdit{op: OpDelete, a: equal.a}, tokenEdit{op: OpInsert, b: equal.b})
				}
				edits = append(append(append([]tokenEdit(nil), edits[:start]...), replaced...), edits[end:]...)
				changed = true
				break
			}
			start = end
		}
	}
	return edits
}

// editWeight is max(deletions, insertions) of the contiguous edit region starting at index and
// extending in direction step.
func editWeight(edits []tokenEdit, index, step int) int {
	deletions, insertions := 0, 0
	for ; index >= 0 && index < len(edits) && edits[index].op != OpEqual; index += step {
		if edits[index].op == OpDelete {
			deletions++
		} else {
			insertions++
		}
	}
	return max(deletions, insertions)
}

// orderRegions puts each edit region's deletions before its insertions.
func orderRegions(edits []tokenEdit) []tokenEdit {
	ordered := make([]tokenEdit, 0, len(edits))
	var insertions []tokenEdit
	for _, current := range edits {
		switch current.op {
		case OpDelete:
			ordered = append(ordered, current)
		case OpInsert:
			insertions = append(insertions, current)
		default:
			ordered = append(append(ordered, insertions...), current)
			insertions = nil
		}
	}
	return append(ordered, insertions...)
}

var linePrefixes = map[Op]string{OpEqual: "  ", OpDelete: "- ", OpInsert: "+ "}

// plain renders the diff as "  ", "- ", and "+ " prefixed lines.
func (diff ShapeDiff) plain(indent string) string {
	var out strings.Builder
	for _, line := range diff {
		out.WriteString(indent + linePrefixes[line.Op] + line.Text + "\n")
	}
	return out.String()
}

// rich renders a replaced pair as one "~ " line with token marks: removed tokens red and struck
// through, added tokens green and bold. Unpaired lines keep their prefix and whole-line style.
// clicky's markdown renderer does not escape text content, so markdown escapes token text here.
func (diff ShapeDiff) rich(indent string, markdown bool) api.Text {
	text := api.Text{}
	for index := 0; index < len(diff); index++ {
		line := diff[index]
		if line.Paired && line.Op == OpDelete && index+1 < len(diff) && diff[index+1].Paired {
			text = text.Append(indent + "~ ")
			for _, segment := range tokenDiff(line.Text, diff[index+1].Text) {
				text = text.Add(styledToken(segment.Op, segment.Text, markdown))
			}
			text = text.Append("\n")
			index++
			continue
		}
		text = text.Append(indent + linePrefixes[line.Op]).Add(styledToken(line.Op, line.Text, markdown)).Append("\n")
	}
	return text
}

var markdownEscaper = strings.NewReplacer(
	`\`, `\\`, "`", "\\`", `*`, `\*`, `_`, `\_`, `~`, `\~`, `[`, `\[`, `]`, `\]`, `<`, `\<`, `>`, `\>`)

// styledToken marks a deleted or inserted run, keeping its surrounding whitespace outside the
// mark so emphasis delimiters stay adjacent to the text they wrap.
func styledToken(op Op, value string, markdown bool) api.Text {
	core := strings.TrimLeft(value, " \t")
	leading := value[:len(value)-len(core)]
	trimmed := strings.TrimRight(core, " \t")
	trailing := core[len(trimmed):]
	if markdown {
		trimmed = markdownEscaper.Replace(trimmed)
	}
	var marked api.Text
	switch op {
	case OpDelete:
		marked = api.NewText(trimmed).Color("red-600").Strikethrough().Build()
	case OpInsert:
		marked = api.NewText(trimmed).Color("green-600").Bold().Build()
	default:
		return api.Text{Content: leading + trimmed + trailing}
	}
	text := api.Text{Content: leading}
	if trimmed != "" {
		text = text.Add(marked)
	}
	if trailing != "" {
		text = text.Append(trailing)
	}
	return text
}

// String is the plain "-"/"+" line form.
func (diff ShapeDiff) String() string { return diff.plain("") }

// ANSI renders token marks for the terminal.
func (diff ShapeDiff) ANSI() string { return diff.rich("", false).ANSI() }

// HTML renders token marks as HTML.
func (diff ShapeDiff) HTML() string { return diff.rich("", false).HTML() }

// Markdown renders token marks as ~~removed~~ and **added**, with token text escaped.
func (diff ShapeDiff) Markdown() string { return diff.rich("", true).Markdown() }

// MarkdownWithOptions renders markdown with clicky's options (NoColor drops colour spans).
func (diff ShapeDiff) MarkdownWithOptions(options api.MarkdownOptions) string {
	return diff.rich("", true).MarkdownWithOptions(options)
}

// indentedShapeDiff places a shape diff under a row: the plain form for String, token marks for
// the rich forms.
type indentedShapeDiff struct {
	diff   ShapeDiff
	indent string
}

func (shape indentedShapeDiff) String() string { return shape.diff.plain(shape.indent) }
func (shape indentedShapeDiff) ANSI() string   { return shape.diff.rich(shape.indent, false).ANSI() }
func (shape indentedShapeDiff) HTML() string   { return shape.diff.rich(shape.indent, false).HTML() }
func (shape indentedShapeDiff) Markdown() string {
	return shape.diff.rich(shape.indent, true).Markdown()
}
func (shape indentedShapeDiff) MarkdownWithOptions(options api.MarkdownOptions) string {
	return shape.diff.rich(shape.indent, true).MarkdownWithOptions(options)
}
