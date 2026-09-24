package storage

import (
	"errors"
	"fmt"
)

var (
	occurrenceRoles     = map[string]bool{"definition": true, "reference": true, "call": true, "write": true, "type": true, "import": true}
	declaredSymbolKinds = map[string]bool{"type": true, "func": true, "method": true, "field": true, "var": true, "const": true}
)

// validateTypedContent checks an indexed or partial document. Every declared symbol has a canonical
// id, a shape hash, and interface ids it implements; a partial document may also keep a declaration
// it could not prove with only its syntax facts and a null id. Every occurrence has a role, a symbol
// or a note saying why it has none, and an enclosing symbol declared in the same document; a call
// also carries its call locator.
func validateTypedContent(content DocumentContent, partial bool) error {
	keys, err := validateSymbolEntries(content.Symbols)
	if err != nil {
		return err
	}
	ids := make(map[string]bool, len(content.Symbols))
	for index, symbol := range content.Symbols {
		subject := fmt.Sprintf("symbol %d (%s)", index, symbol.Key)
		switch {
		case !declaredSymbolKinds[symbol.Kind]:
			return fmt.Errorf("%s: a file cannot declare a %q symbol", subject, symbol.Kind)
		case symbol.ID == nil && (!partial || symbol.ShapeHash != "" || len(symbol.Implements) > 0):
			return fmt.Errorf("%s: only a partial document keeps an unproven declaration, without shape_hash or implements", subject)
		case symbol.ID != nil && (len(*symbol.ID) != 64 || len(symbol.ShapeHash) != 64):
			return fmt.Errorf("%s: a typed symbol requires a 64-character id and shape_hash", subject)
		}
		for _, implemented := range symbol.Implements {
			if len(implemented) != 64 {
				return fmt.Errorf("%s: implements %q is not a symbol id", subject, implemented)
			}
		}
		if symbol.ID != nil {
			ids[*symbol.ID] = true
		}
	}
	for index, occurrence := range content.Occurrences {
		if err := validateTypedOccurrence(occurrence, ids, keys); err != nil {
			return fmt.Errorf("occurrence %d: %w", index, err)
		}
		if index > 0 && occurrence.Bytes[0] < content.Occurrences[index-1].Bytes[0] {
			return fmt.Errorf("occurrence %d: occurrences are not sorted by start byte", index)
		}
	}
	return nil
}

func validateTypedOccurrence(occurrence DocumentOccurrence, ids, keys map[string]bool) error {
	switch {
	case !occurrenceRoles[occurrence.Role]:
		return fmt.Errorf("unknown role %q", occurrence.Role)
	case occurrence.Symbol == nil && occurrence.Note == "":
		return errors.New("an occurrence without a symbol states why in note")
	case occurrence.Symbol != nil && (len(*occurrence.Symbol) != 64 || occurrence.Note != ""):
		return fmt.Errorf("symbol %q is not a symbol id, or carries a note", *occurrence.Symbol)
	case occurrence.Enclosing != nil && !ids[*occurrence.Enclosing]:
		return fmt.Errorf("enclosing %q is not declared in the document", *occurrence.Enclosing)
	case (occurrence.Role == "call") != (occurrence.Target != nil):
		return fmt.Errorf("a %q occurrence carries a call target only when it is a call", occurrence.Role)
	case occurrence.Role == "call" && occurrence.StatementPath == "":
		return errors.New("a call requires a statement path")
	case occurrence.EnclosingKey != "" && !keys[occurrence.EnclosingKey]:
		return fmt.Errorf("enclosing key %q is not declared in the document", occurrence.EnclosingKey)
	}
	return errors.Join(validRange(occurrence.Range), validSpan(occurrence.Bytes))
}
