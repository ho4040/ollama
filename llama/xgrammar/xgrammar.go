//go:build xgrammar

// Package xgrammar exposes the XGrammar constrained-decoding library
// to Go via cgo. It is gated by the `xgrammar` build tag so that the
// default ollama build has no dependency on the static archive
// produced by llama/xgrammar/CMakeLists.txt.
//
// Build the archive once:
//
//	cmake -S llama/xgrammar -B llama/xgrammar/build -DCMAKE_BUILD_TYPE=Release
//	cmake --build llama/xgrammar/build -j
//
// Then build ollama with:
//
//	go build -tags xgrammar ./...
//
// Build command must use `-B llama/xgrammar/build` so cgo's
// $SRCDIR/build path (see LDFLAGS below) resolves; out-of-tree build
// directories are not currently supported.
//
// Resource lifetime: each handle type (TokenizerInfo, Grammar,
// CompiledGrammar, Matcher) has both a runtime finalizer set in its
// constructor and an explicit Free() method. Free() is idempotent —
// it nil-checks the handle, frees the underlying C++ object, nils the
// handle, and clears its own finalizer — so callers may freely call
// Free() on success or error paths without risking a double-free when
// the finalizer later runs.
package xgrammar

/*
#cgo CXXFLAGS: -std=c++17
#cgo CPPFLAGS: -I${SRCDIR}
#cgo LDFLAGS: -L${SRCDIR}/build -lollama_xgrammar
#cgo darwin LDFLAGS: -lc++
#cgo linux LDFLAGS: -lstdc++ -lpthread
#cgo windows LDFLAGS: -lpthread

#include <stdlib.h>
#include "xgrammar_c.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

const errBufSize = 512

// errBufPool reuses error scratch buffers across the hot path
// (FillBitmask, AcceptToken) so we don't pay an allocation per token.
// Buffers are zeroed on return so the next caller sees a clean slate
// for cstr() truncation at the first NUL.
var errBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, errBufSize)
		return &b
	},
}

func getErrBuf() *[]byte {
	bp := errBufPool.Get().(*[]byte)
	b := *bp
	for i := range b {
		b[i] = 0
	}
	return bp
}

func putErrBuf(bp *[]byte) {
	errBufPool.Put(bp)
}

// VocabType selects how token strings in the vocab were encoded.
type VocabType int

const (
	VocabRaw          VocabType = C.XG_VOCAB_RAW
	VocabByteFallback VocabType = C.XG_VOCAB_BYTE_FALLBACK
	VocabByteLevel    VocabType = C.XG_VOCAB_BYTE_LEVEL
)

// TokenizerInfo wraps an xgrammar::TokenizerInfo handle.
type TokenizerInfo struct {
	h         *C.xg_tokenizer_info
	vocabSize int32
}

// NewTokenizerInfo constructs a TokenizerInfo from a list of decoded
// token strings, the EOS/stop token ids, and a vocab encoding hint.
//
// Note: token pieces with embedded NUL bytes are not supported by this
// thin wrapper. If a future model requires binary-safe pieces, extend
// the C ABI to take (lengths[], data[]) instead.
func NewTokenizerInfo(vocab []string, vocabType VocabType, stopTokens []int32, addPrefixSpace bool) (*TokenizerInfo, error) {
	if len(vocab) == 0 {
		return nil, errors.New("xgrammar: empty vocab")
	}

	// C.CString truncates at the first NUL byte, so a token piece
	// containing 0x00 would silently lose data. Detect and reject
	// instead of producing a corrupted TokenizerInfo. The C ABI would
	// need to switch to (lengths[], data[]) pairs to support binary-
	// safe pieces.
	for i, s := range vocab {
		for j := 0; j < len(s); j++ {
			if s[j] == 0 {
				return nil, fmt.Errorf("xgrammar: vocab piece for token id %d contains a NUL byte; binary-safe vocab is not yet supported", i)
			}
		}
	}

	cVocab := make([]*C.char, len(vocab))
	for i, s := range vocab {
		cVocab[i] = C.CString(s)
	}
	defer func() {
		for _, p := range cVocab {
			C.free(unsafe.Pointer(p))
		}
	}()

	var stopPtr *C.int32_t
	if len(stopTokens) > 0 {
		stopPtr = (*C.int32_t)(unsafe.Pointer(&stopTokens[0]))
	}

	bp := getErrBuf()
	defer putErrBuf(bp)
	errBuf := *bp
	h := C.xg_tokenizer_info_new(
		(**C.char)(unsafe.Pointer(&cVocab[0])),
		C.int32_t(len(vocab)),
		C.xg_vocab_type(vocabType),
		stopPtr,
		C.int32_t(len(stopTokens)),
		C.bool(addPrefixSpace),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	if h == nil {
		return nil, fmt.Errorf("xgrammar: TokenizerInfo: %s", cstr(errBuf))
	}

	t := &TokenizerInfo{h: h, vocabSize: int32(len(vocab))}
	runtime.SetFinalizer(t, func(t *TokenizerInfo) { t.Free() })
	return t, nil
}

// VocabSize returns the vocab size that was used to construct the info.
func (t *TokenizerInfo) VocabSize() int32 { return t.vocabSize }

// Free releases the underlying handle. Safe to call multiple times.
func (t *TokenizerInfo) Free() {
	if t == nil || t.h == nil {
		return
	}
	C.xg_tokenizer_info_free(t.h)
	t.h = nil
	runtime.SetFinalizer(t, nil)
}

// Grammar wraps an xgrammar::Grammar handle.
type Grammar struct {
	h *C.xg_grammar
}

// GrammarFromEBNF parses an EBNF/GBNF string into a grammar.
func GrammarFromEBNF(ebnf string) (*Grammar, error) {
	cs := C.CString(ebnf)
	defer C.free(unsafe.Pointer(cs))

	bp := getErrBuf()
	defer putErrBuf(bp)
	errBuf := *bp
	h := C.xg_grammar_from_ebnf(cs, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if h == nil {
		return nil, fmt.Errorf("xgrammar: FromEBNF: %s", cstr(errBuf))
	}
	g := &Grammar{h: h}
	runtime.SetFinalizer(g, func(g *Grammar) { g.Free() })
	return g, nil
}

// GrammarFromJSONSchema converts a JSON schema string into a grammar.
func GrammarFromJSONSchema(schema string, anyWhitespace, strictMode bool) (*Grammar, error) {
	cs := C.CString(schema)
	defer C.free(unsafe.Pointer(cs))

	bp := getErrBuf()
	defer putErrBuf(bp)
	errBuf := *bp
	h := C.xg_grammar_from_json_schema(
		cs,
		C.bool(anyWhitespace),
		C.bool(strictMode),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	if h == nil {
		return nil, fmt.Errorf("xgrammar: FromJSONSchema: %s", cstr(errBuf))
	}
	g := &Grammar{h: h}
	runtime.SetFinalizer(g, func(g *Grammar) { g.Free() })
	return g, nil
}

// Free releases the grammar handle.
func (g *Grammar) Free() {
	if g == nil || g.h == nil {
		return
	}
	C.xg_grammar_free(g.h)
	g.h = nil
	runtime.SetFinalizer(g, nil)
}

// CompiledGrammar wraps an xgrammar::CompiledGrammar handle.
type CompiledGrammar struct {
	h         *C.xg_compiled_grammar
	vocabSize int32
}

// Compile combines a TokenizerInfo with a Grammar to produce a
// CompiledGrammar suitable for matcher construction.
func Compile(t *TokenizerInfo, g *Grammar) (*CompiledGrammar, error) {
	if t == nil || g == nil {
		return nil, errors.New("xgrammar: nil tokenizer or grammar")
	}
	bp := getErrBuf()
	defer putErrBuf(bp)
	errBuf := *bp
	h := C.xg_compile_grammar(t.h, g.h, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if h == nil {
		return nil, fmt.Errorf("xgrammar: Compile: %s", cstr(errBuf))
	}
	cg := &CompiledGrammar{h: h, vocabSize: t.vocabSize}
	runtime.SetFinalizer(cg, func(cg *CompiledGrammar) { cg.Free() })
	return cg, nil
}

// Free releases the compiled grammar handle.
func (cg *CompiledGrammar) Free() {
	if cg == nil || cg.h == nil {
		return
	}
	C.xg_compiled_grammar_free(cg.h)
	cg.h = nil
	runtime.SetFinalizer(cg, nil)
}

// Matcher is a stateful grammar matcher.
type Matcher struct {
	h         *C.xg_matcher
	vocabSize int32
	bitmask   []int32 // reused bitmask scratch
}

// NewMatcher creates a fresh matcher tied to a compiled grammar.
// Created matchers have unlimited rollback history (xgrammar's default).
func NewMatcher(cg *CompiledGrammar) (*Matcher, error) {
	if cg == nil {
		return nil, errors.New("xgrammar: nil compiled grammar")
	}
	bp := getErrBuf()
	defer putErrBuf(bp)
	errBuf := *bp
	h := C.xg_matcher_new(cg.h, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if h == nil {
		return nil, fmt.Errorf("xgrammar: Matcher: %s", cstr(errBuf))
	}
	bmSize := int(C.xg_bitmask_size(C.int32_t(cg.vocabSize)))
	m := &Matcher{
		h:         h,
		vocabSize: cg.vocabSize,
		bitmask:   make([]int32, bmSize),
	}
	runtime.SetFinalizer(m, func(m *Matcher) { m.Free() })
	return m, nil
}

// VocabSize returns the vocab size of the underlying compiled grammar.
func (m *Matcher) VocabSize() int32 { return m.vocabSize }

// FillBitmask refreshes the matcher's internal bitmask scratch with
// the set of currently-allowed tokens, and returns it as a slice.
//
// The returned slice is owned by the matcher and is overwritten on
// every call. Callers should not retain it across FillBitmask calls.
//
// The second return value is true when the bitmask is meaningful
// (i.e. some tokens are rejected); false signals that every token is
// allowed and the caller may skip masking.
func (m *Matcher) FillBitmask() ([]int32, bool, error) {
	bp := getErrBuf()
	defer putErrBuf(bp)
	errBuf := *bp
	rc := C.xg_matcher_fill_next_token_bitmask(
		m.h,
		(*C.int32_t)(unsafe.Pointer(&m.bitmask[0])),
		C.int32_t(m.vocabSize),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	switch rc {
	case 1:
		return m.bitmask, true, nil
	case 0:
		return m.bitmask, false, nil
	default:
		return nil, false, fmt.Errorf("xgrammar: FillBitmask: %s", cstr(errBuf))
	}
}

// Allowed reports whether token id `tok` is allowed by the bitmask
// returned from FillBitmask.
func Allowed(bitmask []int32, tok int32) bool {
	idx := tok / 32
	if idx < 0 || int(idx) >= len(bitmask) {
		return false
	}
	return bitmask[idx]&(1<<uint(tok%32)) != 0
}

// AcceptToken feeds a sampled token back into the matcher.
func (m *Matcher) AcceptToken(tok int32) error {
	bp := getErrBuf()
	defer putErrBuf(bp)
	errBuf := *bp
	rc := C.xg_matcher_accept_token(
		m.h,
		C.int32_t(tok),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	switch rc {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("xgrammar: token %d rejected", tok)
	default:
		return fmt.Errorf("xgrammar: AcceptToken: %s", cstr(errBuf))
	}
}

// Rollback rolls the matcher back by n tokens.
func (m *Matcher) Rollback(n int) error {
	bp := getErrBuf()
	defer putErrBuf(bp)
	errBuf := *bp
	rc := C.xg_matcher_rollback(
		m.h,
		C.int32_t(n),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	if rc != 0 {
		return fmt.Errorf("xgrammar: Rollback: %s", cstr(errBuf))
	}
	return nil
}

// Reset returns the matcher to its initial state.
func (m *Matcher) Reset() { C.xg_matcher_reset(m.h) }

// IsTerminated reports whether the matcher accepted a stop token.
func (m *Matcher) IsTerminated() bool { return bool(C.xg_matcher_is_terminated(m.h)) }

// IsCompleted reports whether the root rule has been fully matched.
func (m *Matcher) IsCompleted() bool { return bool(C.xg_matcher_is_completed(m.h)) }

// Free releases the matcher handle.
func (m *Matcher) Free() {
	if m == nil || m.h == nil {
		return
	}
	C.xg_matcher_free(m.h)
	m.h = nil
	runtime.SetFinalizer(m, nil)
}

// cstr converts a null-terminated byte buffer (typically the err_buf
// scratch passed into the C wrapper) into a trimmed Go string,
// stopping at the first NUL.
func cstr(buf []byte) string {
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}
