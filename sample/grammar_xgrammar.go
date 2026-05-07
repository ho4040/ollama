//go:build xgrammar

package sample

import (
	"log/slog"
	"math"
	"os"
	"strings"

	"github.com/ollama/ollama/llama/xgrammar"
	"github.com/ollama/ollama/tokenizer"
)

// xgrammarBackend adapts xgrammar's matcher to the Grammar interface
// expected by the new-engine Sampler. Selected via
// OLLAMA_GRAMMAR_BACKEND=xgrammar.
//
// When the request originated from a JSON schema (format=<object>),
// the server propagates the raw schema alongside the GBNF string and
// this backend prefers GrammarFromJSONSchema. The legacy GBNF input
// remains as a fallback so format="json" (which has no schema) and
// any caller-supplied raw GBNF still work.
//
// llama.cpp's GBNF dialect is not a strict subset of XGrammar's EBNF
// (e.g. top-level `|` alternation is rejected), so the schema-direct
// path is the only one we expect to succeed for typical structured
// output requests.
type xgrammarBackend struct {
	tok     *xgrammar.TokenizerInfo
	gram    *xgrammar.Grammar
	cg      *xgrammar.CompiledGrammar
	matcher *xgrammar.Matcher
}

// freeable abstracts the four xgrammar handle types so we can clean up
// already-allocated resources on a partial-construction error in one
// pass rather than open-coding cascading Free() calls per failure
// point.
type freeable interface{ Free() }

// freeAll calls Free() on each non-nil handle. Each xgrammar handle's
// Free() is itself idempotent (nil-checks, frees, then nils its h
// pointer and clears its finalizer), so callers may pass a slice that
// includes typed-nil entries safely.
func freeAll(handles ...freeable) {
	for _, h := range handles {
		if h != nil {
			h.Free()
		}
	}
}

// detectVocabType picks an xgrammar VocabType from the shape of the
// decoded vocabulary. The heuristic looks for the two well-known
// markers:
//
//   - SentencePiece byte-fallback exposes 256 literal "<0xHH>" tokens
//     (Llama 1/2/3, Mistral, Mixtral, ...).
//   - GPT-2 byte-level BPE replaces raw bytes 0..255 with a fixed set
//     of glyphs; 'Ġ' (U+0120) substitutes the space byte and is by
//     far the most common token among them (Qwen, GPT-2, ...).
//
// Vocabularies that match neither (e.g. Gemma's Unigram-style pieces)
// fall through to RAW, which matches xgrammar's behaviour for
// "decoded piece text is already the surface form".
//
// The thresholds are deliberately loose: detection runs once per
// request, vocab sizes are 30k-200k, and the cost of a wrong answer
// is silent grammar mismatch rather than a crash. Override via
// OLLAMA_XGRAMMAR_VOCAB_TYPE if a future tokenizer trips the
// heuristic.
func detectVocabType(pieces []string) xgrammar.VocabType {
	var byteFallbackHits, byteLevelHits int
	for _, p := range pieces {
		if len(p) == 6 && p[0] == '<' && p[1] == '0' && p[2] == 'x' && p[5] == '>' {
			byteFallbackHits++
		}
		if strings.ContainsRune(p, 'Ġ') || strings.ContainsRune(p, 'Ċ') {
			byteLevelHits++
		}
	}
	switch {
	case byteFallbackHits >= 200:
		return xgrammar.VocabByteFallback
	case byteLevelHits >= 100:
		return xgrammar.VocabByteLevel
	default:
		return xgrammar.VocabRaw
	}
}

// selectVocabType resolves the vocab type for a given decoded
// vocabulary. OLLAMA_XGRAMMAR_VOCAB_TYPE forces a specific value
// ("raw", "byte_fallback", "byte_level"); otherwise the auto-detector
// decides.
func selectVocabType(pieces []string) xgrammar.VocabType {
	switch os.Getenv("OLLAMA_XGRAMMAR_VOCAB_TYPE") {
	case "raw":
		return xgrammar.VocabRaw
	case "byte_fallback":
		return xgrammar.VocabByteFallback
	case "byte_level":
		return xgrammar.VocabByteLevel
	}
	return detectVocabType(pieces)
}

func newXGrammar(tok tokenizer.Tokenizer, grammarStr, schema string) (Grammar, error) {
	vocab := tok.Vocabulary().Values
	pieces := make([]string, len(vocab))
	for i := range vocab {
		pieces[i], _ = tok.Decode([]int32{int32(i)})
	}

	vt := selectVocabType(pieces)
	slog.Debug("xgrammar tokenizer info", "vocab_type", vt, "vocab_size", len(pieces))

	// Cleanup contract: each xgrammar.*.Free() is idempotent (nil-checks
	// the handle, frees the C++ object, nils the handle, and clears its
	// own finalizer), so an explicit Free() on these error paths is
	// safe even though the constructor already attached a runtime
	// finalizer to the same object. The explicit Free() releases the
	// allocation immediately rather than waiting for GC; the (now
	// no-op) finalizer fires later without double-freeing.
	info, err := xgrammar.NewTokenizerInfo(pieces, vt, tok.Vocabulary().EOS, false)
	if err != nil {
		return nil, err
	}

	var g *xgrammar.Grammar
	if schema != "" {
		g, err = xgrammar.GrammarFromJSONSchema(schema, true, true)
	} else {
		g, err = xgrammar.GrammarFromEBNF(grammarStr)
	}
	if err != nil {
		freeAll(info)
		return nil, err
	}

	cg, err := xgrammar.Compile(info, g)
	if err != nil {
		freeAll(g, info)
		return nil, err
	}

	m, err := xgrammar.NewMatcher(cg)
	if err != nil {
		freeAll(cg, g, info)
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
	// id>>5 picks the int32 word; id&31 picks the bit within it.
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

func (b *xgrammarBackend) Backend() string { return "xgrammar" }
