// Package contenttype shares the editor's recognized code languages with search.
package contenttype

import (
	_ "embed"
	"encoding/json"
)

//go:embed languages.json
var registry []byte

// Tokens includes picker IDs, serialized fence tokens, and historical aliases.
func Tokens() []string {
	var languages []struct {
		ID            string   `json:"id"`
		FenceLanguage string   `json:"fenceLanguage"`
		Aliases       []string `json:"aliases"`
	}
	if err := json.Unmarshal(registry, &languages); err != nil {
		panic(err)
	}
	var tokens []string
	for _, language := range languages {
		tokens = append(tokens, language.ID, language.FenceLanguage)
		tokens = append(tokens, language.Aliases...)
	}
	return tokens
}
