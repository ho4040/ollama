//go:build xgrammar

package sample

import (
	"math"
	"math/rand/v2"
	"testing"
)

// TestXGrammarDispatchAndJSONMask exercises the Phase 1 integration
// end-to-end: dispatcher selects the xgrammar backend when the env
// var is set, the same JSON GBNF that the legacy backend uses parses
// under XGrammar's EBNF, and Apply() actually masks at least one
// token that the grammar would reject.
func TestXGrammarDispatchAndJSONMask(t *testing.T) {
	t.Setenv("OLLAMA_GRAMMAR_BACKEND", "xgrammar")
	tk := modelHelper(t)

	const grammarJSON = `
	root   ::= object
	value  ::= object | array | string | number | ("true" | "false" | "null") ws
	object ::=
	"{" ws (
				string ":" ws value
		("," ws string ":" ws value)*
	)? "}" ws
	array  ::=
	"[" ws (
				value
		("," ws value)*
	)? "]" ws
	string ::=
	"\"" (
		[^"\\\x7F\x00-\x1F] |
		"\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F])
	)* "\"" ws
	number ::= ("-"? ([0-9] | [1-9] [0-9]*)) ("." [0-9]+)? ([eE] [-+]? [0-9]+)? ws
	ws ::= ([ \t\n] ws)?
	`

	g, err := NewGrammarSampler(tk, grammarJSON)
	if err != nil {
		t.Fatalf("NewGrammarSampler with xgrammar: %v", err)
	}
	defer g.Free()

	if _, ok := g.(*xgrammarBackend); !ok {
		t.Fatalf("dispatcher returned %T; expected *xgrammarBackend", g)
	}

	// Build a fake-logits batch and apply the mask. We expect the
	// grammar to reject at least one token at the JSON-start state
	// (anything that isn't whitespace or '{' must be -inf).
	logits := make([]float32, len(tk.Vocabulary().Values))
	for i := range logits {
		logits[i] = rand.Float32()
	}
	tokens := make([]token, len(logits))
	for i := range tokens {
		tokens[i] = token{id: int32(i), value: logits[i]}
	}

	g.Apply(tokens)

	infCount, finiteCount := 0, 0
	for _, tk := range tokens {
		if math.IsInf(float64(tk.value), -1) {
			infCount++
		} else {
			finiteCount++
		}
	}
	if infCount == 0 {
		t.Error("expected at least one rejected token at JSON start, got none")
	}
	if finiteCount == 0 {
		t.Error("expected at least one accepted token at JSON start, got none (mask may be over-restrictive)")
	}
	t.Logf("xgrammar JSON-start mask: %d allowed, %d rejected (vocab=%d)", finiteCount, infCount, len(tokens))
}
