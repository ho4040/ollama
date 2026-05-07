//go:build xgrammar

package xgrammar

import (
	"strings"
	"testing"
)

// minimalVocab is intentionally tiny (11 tokens) for fast unit tests; large-vocab integration coverage lives in the sample package.
// The integration test below relies only on these specific token strings; the first entry is the EOS.
func minimalVocab() ([]string, []int32) {
	pieces := []string{
		"</s>", // 0  EOS
		"{",    // 1
		"}",    // 2
		"\"",   // 3
		":",    // 4
		"a",    // 5
		"b",    // 6
		"1",    // 7
		"2",    // 8
		" ",    // 9
		",",    // 10
	}
	return pieces, []int32{0}
}

func TestEBNFMatcher(t *testing.T) {
	vocab, stops := minimalVocab()

	tok, err := NewTokenizerInfo(vocab, VocabRaw, stops, false)
	if err != nil {
		t.Fatalf("NewTokenizerInfo: %v", err)
	}
	defer tok.Free()

	// Trivial grammar: accept exactly the literal "ab".
	g, err := GrammarFromEBNF(`root ::= "ab"`)
	if err != nil {
		t.Fatalf("GrammarFromEBNF: %v", err)
	}
	defer g.Free()

	cg, err := Compile(tok, g)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer cg.Free()

	m, err := NewMatcher(cg)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	defer m.Free()

	// At the start, only "a" (token 5) should be accepted.
	mask, need, err := m.FillBitmask()
	if err != nil {
		t.Fatalf("FillBitmask #1: %v", err)
	}
	if !need {
		t.Fatal("expected mask to be meaningful at start")
	}
	if !Allowed(mask, 5) {
		t.Error("token 'a' should be allowed at start")
	}
	if Allowed(mask, 6) {
		t.Error("token 'b' should NOT be allowed at start")
	}

	if err := m.AcceptToken(5); err != nil {
		t.Fatalf("AcceptToken a: %v", err)
	}

	// Now only "b" (token 6) should be accepted.
	mask, _, err = m.FillBitmask()
	if err != nil {
		t.Fatalf("FillBitmask #2: %v", err)
	}
	if Allowed(mask, 5) {
		t.Error("token 'a' should NOT be allowed after consuming 'a'")
	}
	if !Allowed(mask, 6) {
		t.Error("token 'b' should be allowed after consuming 'a'")
	}

	if err := m.AcceptToken(6); err != nil {
		t.Fatalf("AcceptToken b: %v", err)
	}

	if !m.IsCompleted() {
		t.Error("expected matcher to be completed after 'ab'")
	}

	// Stop token should now be the only legal next token.
	if err := m.AcceptToken(0); err != nil {
		t.Fatalf("AcceptToken EOS: %v", err)
	}
	if !m.IsTerminated() {
		t.Error("expected matcher to be terminated after EOS")
	}
}

func TestJSONSchemaMatcher(t *testing.T) {
	vocab, stops := minimalVocab()

	tok, err := NewTokenizerInfo(vocab, VocabRaw, stops, false)
	if err != nil {
		t.Fatalf("NewTokenizerInfo: %v", err)
	}
	defer tok.Free()

	schema := `{"type":"object","properties":{"a":{"type":"integer"}},"required":["a"]}`
	g, err := GrammarFromJSONSchema(schema, true, true)
	if err != nil {
		t.Fatalf("GrammarFromJSONSchema: %v", err)
	}
	defer g.Free()

	cg, err := Compile(tok, g)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer cg.Free()

	m, err := NewMatcher(cg)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	defer m.Free()

	mask, need, err := m.FillBitmask()
	if err != nil {
		t.Fatalf("FillBitmask: %v", err)
	}
	if !need {
		t.Fatal("expected mask to be meaningful at start of JSON object")
	}
	if !Allowed(mask, 1) {
		t.Error("token '{' should be allowed as the first token")
	}
	if Allowed(mask, 2) {
		t.Error("token '}' should NOT be allowed as the first token")
	}
}

func TestAllowedBoundsCheck(t *testing.T) {
	mask := []int32{-1} // all bits set
	if Allowed(mask, -1) {
		t.Error("Allowed(mask, -1) should be false")
	}
	if Allowed(mask, 32) {
		t.Error("Allowed(mask, 32) should be false (out-of-bounds)")
	}
	if !Allowed(mask, 0) {
		t.Error("Allowed(mask, 0) should be true")
	}
}

func TestNewTokenizerInfoEmptyVocab(t *testing.T) {
	_, err := NewTokenizerInfo([]string{}, VocabRaw, nil, false)
	if err == nil {
		t.Fatal("expected error for empty vocab, got nil")
	}
	if !strings.Contains(err.Error(), "empty vocab") {
		t.Errorf("unexpected error string: %v", err)
	}
}

func TestFreeIsIdempotent(t *testing.T) {
	vocab, stops := minimalVocab()

	tok, err := NewTokenizerInfo(vocab, VocabRaw, stops, false)
	if err != nil {
		t.Fatalf("NewTokenizerInfo: %v", err)
	}
	tok.Free()
	tok.Free()

	tok2, err := NewTokenizerInfo(vocab, VocabRaw, stops, false)
	if err != nil {
		t.Fatalf("NewTokenizerInfo: %v", err)
	}
	defer tok2.Free()

	g, err := GrammarFromEBNF(`root ::= "ab"`)
	if err != nil {
		t.Fatalf("GrammarFromEBNF: %v", err)
	}
	cg, err := Compile(tok2, g)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	m, err := NewMatcher(cg)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	m.Free()
	m.Free()
	cg.Free()
	cg.Free()
	g.Free()
	g.Free()
}

func TestRejectedTokenSurfacesError(t *testing.T) {
	vocab, stops := minimalVocab()
	tok, _ := NewTokenizerInfo(vocab, VocabRaw, stops, false)
	defer tok.Free()
	g, _ := GrammarFromEBNF(`root ::= "ab"`)
	defer g.Free()
	cg, _ := Compile(tok, g)
	defer cg.Free()
	m, _ := NewMatcher(cg)
	defer m.Free()

	err := m.AcceptToken(6) // 'b' before 'a' must fail
	if err == nil {
		t.Fatal("expected rejection error, got nil")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("unexpected error string: %v", err)
	}
}
