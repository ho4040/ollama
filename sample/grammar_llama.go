package sample

import (
	"errors"

	"github.com/ollama/ollama/llama"
	"github.com/ollama/ollama/tokenizer"
)

// llamaBackend is the legacy GBNF backend backed by llama.cpp.
type llamaBackend struct {
	grammar *llama.Grammar
}

func newLlamaGrammar(tok tokenizer.Tokenizer, grammarStr string) (Grammar, error) {
	vocabIds := make([]uint32, len(tok.Vocabulary().Values))
	pieces := make([]string, len(tok.Vocabulary().Values))
	for i := range tok.Vocabulary().Values {
		pieces[i], _ = tok.Decode([]int32{int32(i)})
		vocabIds[i] = uint32(i)
	}

	grammar := llama.NewGrammar(grammarStr, vocabIds, pieces, tok.Vocabulary().EOS)
	if grammar == nil {
		return nil, errors.New("sample: failed to initialize grammar")
	}
	return &llamaBackend{grammar: grammar}, nil
}

func (g *llamaBackend) Apply(tokens []token) {
	tds := make([]llama.TokenData, len(tokens))
	for i, t := range tokens {
		tds[i].ID = t.id
		tds[i].Logit = t.value
	}
	g.grammar.Apply(tds)
	for i := range tokens {
		tokens[i].value = tds[i].Logit
	}
}

func (g *llamaBackend) Accept(id int32) { g.grammar.Accept(id) }

func (g *llamaBackend) Free() { g.grammar.Free() }

func (g *llamaBackend) Backend() string { return "gbnf" }
