package sample

import "github.com/ollama/ollama/tokenizer"

// Grammar abstracts the constrained-decoding backend used by Sampler.
//
// The default backend is GBNF via llama.cpp (see grammar_llama.go).
// An optional XGrammar backend lives behind the `xgrammar` build tag
// and is selected at runtime via OLLAMA_GRAMMAR_BACKEND=xgrammar.
type Grammar interface {
	// Apply masks tokens that would violate the grammar by setting
	// their logit to -Inf. The slice is modified in place.
	Apply(tokens []token)

	// Accept advances the grammar state with the given token id.
	Accept(id int32)

	// Free releases any backend-held resources.
	Free()
}

// NewGrammarSampler constructs the configured grammar backend.
//
// The default is the legacy GBNF/llama backend so that ollama's
// behaviour is unchanged unless the user opts in via:
//
//	OLLAMA_GRAMMAR_BACKEND=xgrammar
//
// When the env var requests xgrammar but the binary was built without
// the `xgrammar` build tag, this falls back to the GBNF backend with
// a single warning so the request still serves.
//
// schema is the raw JSON schema if the request was driven by
// format=<schema>; backends that compile schemas directly (xgrammar)
// prefer it. grammarStr is the GBNF form ollama already produces and
// is the only input the legacy backend understands.
func NewGrammarSampler(tok tokenizer.Tokenizer, grammarStr, schema string) (Grammar, error) {
	return newGrammarSampler(tok, grammarStr, schema)
}
