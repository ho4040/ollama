//go:build !xgrammar

package sample

import (
	"log/slog"
	"os"

	"github.com/ollama/ollama/tokenizer"
)

// newGrammarSampler is the default-build dispatcher. Without the
// `xgrammar` build tag only the GBNF backend is available; an
// OLLAMA_GRAMMAR_BACKEND=xgrammar request from a user gets a single
// warning and falls back to GBNF so the request still serves.
func newGrammarSampler(tok tokenizer.Tokenizer, grammarStr string) (Grammar, error) {
	if backend := os.Getenv("OLLAMA_GRAMMAR_BACKEND"); backend == "xgrammar" {
		slog.Warn("OLLAMA_GRAMMAR_BACKEND=xgrammar requested but ollama was built without the xgrammar tag; falling back to GBNF")
	}
	return newLlamaGrammar(tok, grammarStr)
}
