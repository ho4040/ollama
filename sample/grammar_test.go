//go:build !xgrammar

package sample

import "testing"

// TestGrammarDispatcherFallback verifies that when the user requests
// the xgrammar backend via OLLAMA_GRAMMAR_BACKEND=xgrammar but the
// binary was built without the `xgrammar` build tag, the dispatcher
// falls back to the GBNF backend instead of erroring out.
//
// In the default build this exercises grammar_dispatch_default.go's
// fallback path; the xgrammar-tagged build is covered separately by
// grammar_xgrammar_test.go.
func TestGrammarDispatcherFallback(t *testing.T) {
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

	g, err := NewGrammarSampler(tk, grammarJSON, "")
	if err != nil {
		t.Fatalf("NewGrammarSampler: %v", err)
	}
	defer g.Free()

	// Constrained to the default build by the //go:build !xgrammar
	// tag above: the only valid backend here is "gbnf", and a
	// returned "xgrammar" would mean the fallback warning path was
	// skipped. The xgrammar-tagged build is covered by
	// grammar_xgrammar_test.go.
	if got := g.Backend(); got != "gbnf" {
		t.Errorf("dispatcher fallback Backend() = %q; want %q", got, "gbnf")
	}
}
