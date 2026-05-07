//go:build xgrammar

package sample

import (
	"math"

	"github.com/ollama/ollama/llama/xgrammar"
	"github.com/ollama/ollama/tokenizer"
)

// xgrammarBackend adapts xgrammar's matcher to the Grammar interface
// expected by the new-engine Sampler. Selected via
// OLLAMA_GRAMMAR_BACKEND=xgrammar.
//
// Phase 1: the GBNF string produced by ollama (via
// llama.SchemaToGrammar for JSON schema requests, or the static
// grammarJSON for format=json) is passed to XGrammar's EBNF parser.
// XGrammar accepts a superset of the dialects we emit; if a request
// turns out to use a construct XGrammar can't parse, the dispatcher
// falls back to the GBNF backend with a warning.
type xgrammarBackend struct {
	tok     *xgrammar.TokenizerInfo
	gram    *xgrammar.Grammar
	cg      *xgrammar.CompiledGrammar
	matcher *xgrammar.Matcher
}

func newXGrammar(tok tokenizer.Tokenizer, grammarStr string) (Grammar, error) {
	vocab := tok.Vocabulary().Values
	pieces := make([]string, len(vocab))
	for i := range vocab {
		pieces[i], _ = tok.Decode([]int32{int32(i)})
	}

	info, err := xgrammar.NewTokenizerInfo(pieces, xgrammar.VocabRaw, tok.Vocabulary().EOS, false)
	if err != nil {
		return nil, err
	}

	g, err := xgrammar.GrammarFromEBNF(grammarStr)
	if err != nil {
		info.Free()
		return nil, err
	}

	cg, err := xgrammar.Compile(info, g)
	if err != nil {
		g.Free()
		info.Free()
		return nil, err
	}

	m, err := xgrammar.NewMatcher(cg)
	if err != nil {
		cg.Free()
		g.Free()
		info.Free()
		return nil, err
	}

	return &xgrammarBackend{tok: info, gram: g, cg: cg, matcher: m}, nil
}

func (b *xgrammarBackend) Apply(tokens []token) {
	mask, need, err := b.matcher.FillBitmask()
	if err != nil || !need {
		// If the call errored we conservatively reject everything;
		// callers will surface the error on the next AcceptToken.
		// If the mask isn't meaningful, every token is allowed and
		// there is nothing to mask.
		if err != nil {
			for i := range tokens {
				tokens[i].value = float32(math.Inf(-1))
			}
		}
		return
	}
	negInf := float32(math.Inf(-1))
	for i := range tokens {
		id := tokens[i].id
		idx := id >> 5
		if idx < 0 || int(idx) >= len(mask) {
			tokens[i].value = negInf
			continue
		}
		if mask[idx]&(1<<uint(id&31)) == 0 {
			tokens[i].value = negInf
		}
	}
}

func (b *xgrammarBackend) Accept(id int32) {
	// AcceptToken can fail (e.g. if the sampled token violates the
	// grammar). In that case we reset the matcher to keep the next
	// step consistent — the misbehaviour will surface as an
	// all-rejected mask, which the caller already treats as terminal.
	if err := b.matcher.AcceptToken(id); err != nil {
		b.matcher.Reset()
	}
}

func (b *xgrammarBackend) Free() {
	if b.matcher != nil {
		b.matcher.Free()
	}
	if b.cg != nil {
		b.cg.Free()
	}
	if b.gram != nil {
		b.gram.Free()
	}
	if b.tok != nil {
		b.tok.Free()
	}
}
