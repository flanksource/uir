package lint

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/flanksource/uir/query"
)

type messageSymbol map[string]any

func (symbol messageSymbol) String() string { return symbol["query_name"].(string) }

func (rule Rule) renderMessage(row query.QueryRow, path string) (string, error) {
	bindings := make(map[string]any, len(row.Values)+2)
	maps.Copy(bindings, row.Values)
	if !row.SymbolAlias {
		bindings["symbol"] = nil
		if row.Symbol != nil {
			symbol, err := bindMessageSymbol(*row.Symbol, path, row.Match.Line)
			if err != nil {
				return "", err
			}
			bindings["symbol"] = symbol
		}
	}
	if _, exists := bindings["kind"]; !exists {
		bindings["kind"] = row.Properties
		if row.Properties == nil {
			bindings["kind"] = map[string]any{}
		}
	}
	var message strings.Builder
	err := rule.message.Execute(&message, bindings)
	return message.String(), err
}

func bindMessageSymbol(symbol query.ModuleSymbol, path string, line *int) (messageSymbol, error) {
	params := []string{}
	if len(symbol.ParameterTypes) > 0 {
		if err := json.Unmarshal(symbol.ParameterTypes, &params); err != nil {
			return nil, fmt.Errorf("decode parameters of %s: %w", symbol.QueryName, err)
		}
	}
	path = filepath.ToSlash(path)
	filename, location := "", path
	var lineValue any
	if line != nil {
		lineValue = *line
	}
	if path != "" {
		filename = filepath.Base(path)
		if line != nil {
			location += ":" + strconv.Itoa(*line)
		}
	}
	return messageSymbol{
		"module": symbol.ModuleKey, "package": symbol.PackagePath, "kind": symbol.Kind, "name": symbol.Name,
		"query_name": symbol.QueryName, "params": params,
		"filename": filename, "filepath": path, "line": lineValue, "location": location,
	}, nil
}
