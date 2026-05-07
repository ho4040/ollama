//go:build xgrammar

package sample

import (
	"log/slog"
	"os"

	"github.com/ollama/ollama/tokenizer"
)

// newGrammarSampler dispatches to the XGrammar backend when the user
// opts in via OLLAMA_GRAMMAR_BACKEND=xgrammar. Default remains GBNF
// so behaviour is unchanged for existing deployments that simply
// recompile with the `xgrammar` build tag.
func newGrammarSampler(tok tokenizer.Tokenizer, grammarStr string) (Grammar, error) {
	switch os.Getenv("OLLAMA_GRAMMAR_BACKEND") {
	case "xgrammar":
		g, err := newXGrammar(tok, grammarStr)
		if err != nil {
			slog.Warn("xgrammar backend init failed, falling back to GBNF", "error", err)
			return newLlamaGrammar(tok, grammarStr)
		}
		return g, nil
	default:
		return newLlamaGrammar(tok, grammarStr)
	}
}
