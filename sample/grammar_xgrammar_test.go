//go:build xgrammar

package sample

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/ollama/ollama/llama/xgrammar"
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

	g, err := NewGrammarSampler(tk, grammarJSON, "")
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
	rng := rand.New(rand.NewPCG(1, 2))
	logits := make([]float32, len(tk.Vocabulary().Values))
	for i := range logits {
		logits[i] = rng.Float32()
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

// TestXGrammarSchemaDirect exercises the Phase 2 path: when the
// caller passes a raw JSON schema, the backend should bypass the
// llama.cpp GBNF round-trip (which XGrammar's EBNF parser cannot
// fully accept due to dialect differences) and compile the schema
// directly via Grammar::FromJSONSchema.
//
// Regression guard: a Phase 1 implementation that fed the GBNF to
// FromEBNF failed at runtime with "Expect element, but got |"
// against ollama's emitted GBNF.
func TestXGrammarSchemaDirect(t *testing.T) {
	t.Setenv("OLLAMA_GRAMMAR_BACKEND", "xgrammar")
	tk := modelHelper(t)

	const schema = `{"type":"object","required":["name","age"],"properties":{"name":{"type":"string"},"age":{"type":"integer"}}}`

	// Pass an empty grammarStr to prove the backend used the
	// schema, not the GBNF.
	g, err := NewGrammarSampler(tk, "", schema)
	if err != nil {
		t.Fatalf("NewGrammarSampler with schema: %v", err)
	}
	defer g.Free()

	if _, ok := g.(*xgrammarBackend); !ok {
		t.Fatalf("dispatcher returned %T; expected *xgrammarBackend", g)
	}

	rng := rand.New(rand.NewPCG(1, 2))
	logits := make([]float32, len(tk.Vocabulary().Values))
	for i := range logits {
		logits[i] = rng.Float32()
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
		t.Error("expected at least one rejected token at JSON-start, got none")
	}
	if finiteCount == 0 {
		t.Error("expected at least one accepted token at JSON-start, got none")
	}
	t.Logf("schema-direct mask: %d allowed, %d rejected (vocab=%d)", finiteCount, infCount, len(tokens))
}

func TestDetectVocabType(t *testing.T) {
	// SentencePiece byte-fallback: 256 "<0xHH>" tokens plus regular pieces.
	bf := make([]string, 0, 300)
	for i := 0; i < 256; i++ {
		bf = append(bf, fmtHex(i))
	}
	bf = append(bf, "the", "of", "and", "▁hello")
	if got := detectVocabType(bf); got != xgrammar.VocabByteFallback {
		t.Errorf("byte-fallback vocab: got %d want %d", got, xgrammar.VocabByteFallback)
	}

	// GPT-2 byte-level: many pieces include Ġ.
	bl := []string{"the", "Ġthe", "Ġof", "Ġand", "Ġa", "Ġto", "Ġin", "Ġis"}
	for i := 0; i < 200; i++ {
		bl = append(bl, "Ġtoken"+rune3(i))
	}
	if got := detectVocabType(bl); got != xgrammar.VocabByteLevel {
		t.Errorf("byte-level vocab: got %d want %d", got, xgrammar.VocabByteLevel)
	}

	// Raw / SentencePiece Unigram: no special markers.
	raw := []string{"hello", "world", "foo", "bar", "▁hello", "▁world"}
	if got := detectVocabType(raw); got != xgrammar.VocabRaw {
		t.Errorf("raw vocab: got %d want %d", got, xgrammar.VocabRaw)
	}
}

func TestSelectVocabTypeOverride(t *testing.T) {
	raw := []string{"hello", "world"}

	t.Setenv("OLLAMA_XGRAMMAR_VOCAB_TYPE", "byte_fallback")
	if got := selectVocabType(raw); got != xgrammar.VocabByteFallback {
		t.Errorf("override byte_fallback: got %d", got)
	}

	t.Setenv("OLLAMA_XGRAMMAR_VOCAB_TYPE", "byte_level")
	if got := selectVocabType(raw); got != xgrammar.VocabByteLevel {
		t.Errorf("override byte_level: got %d", got)
	}

	t.Setenv("OLLAMA_XGRAMMAR_VOCAB_TYPE", "raw")
	if got := selectVocabType(raw); got != xgrammar.VocabRaw {
		t.Errorf("override raw: got %d", got)
	}

	t.Setenv("OLLAMA_XGRAMMAR_VOCAB_TYPE", "")
	if got := selectVocabType(raw); got != xgrammar.VocabRaw {
		t.Errorf("auto-detect on raw: got %d", got)
	}
}

// fmtHex builds the literal "<0xHH>" SentencePiece byte token.
func fmtHex(b int) string {
	const hex = "0123456789ABCDEF"
	return "<0x" + string(hex[b>>4]) + string(hex[b&0xf]) + ">"
}

func rune3(i int) string {
	return string(rune('A'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('0'+i%10))
}
