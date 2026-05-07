// Copyright (c) 2026 The Ollama Authors
//
// Thin C ABI over the XGrammar C++ public headers so it can be consumed
// from Go via cgo. Only the surface area needed by ollama's sampler is
// exposed: tokenizer info, grammar construction (EBNF and JSON schema),
// matcher state, and bitmask read-out.
//
// All handles are opaque pointers; ownership is held by the caller and
// must be released with the matching xg_*_free function.
//
// Error buffer policy: every function that reports failure takes an
// (err_buf, err_len) pair. On error, the wrapper writes a NUL-terminated
// human-readable message into err_buf, truncating to err_len-1 bytes if
// the underlying message is longer (so callers can rely on a NUL
// terminator). Pass err_buf=NULL or err_len=0 to discard the message.

#ifndef OLLAMA_XGRAMMAR_C_H_
#define OLLAMA_XGRAMMAR_C_H_

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct xg_tokenizer_info xg_tokenizer_info;
typedef struct xg_grammar xg_grammar;
typedef struct xg_compiled_grammar xg_compiled_grammar;
typedef struct xg_matcher xg_matcher;

// Vocab encoding hint passed to TokenizerInfo. Mirrors xgrammar::VocabType.
//
// Choose based on the model's tokenizer family:
//   - XG_VOCAB_RAW: SentencePiece/Unigram-style models where the
//     decoded piece text is already the surface form (e.g. Gemma).
//   - XG_VOCAB_BYTE_FALLBACK: SentencePiece BPE with byte-fallback
//     tokens (e.g. Llama, Mistral).
//   - XG_VOCAB_BYTE_LEVEL: byte-level BPE (e.g. GPT-2/Qwen variants
//     using the GPT-2 byte-to-unicode mapping).
typedef enum {
  XG_VOCAB_RAW = 0,
  XG_VOCAB_BYTE_FALLBACK = 1,
  XG_VOCAB_BYTE_LEVEL = 2,
} xg_vocab_type;

// Construct a TokenizerInfo from a vocabulary array.
//   vocab           : array of NUL-terminated strings, length vocab_size.
//                     Each entry is the decoded text for token id i.
//                     NOTE: NUL bytes inside a token piece are not supported
//                     here. If callers need binary-safe pieces, switch this
//                     to a (lengths[], data[]) pair in a follow-up.
//   vocab_size      : number of entries in vocab.
//   vocab_type      : how the vocab strings are encoded.
//   stop_token_ids  : array of stop/EOS token ids (may be NULL if 0).
//   stop_count      : length of stop_token_ids.
//   add_prefix_space: tokenizer prefix-space behaviour.
//   err_buf/err_len : optional error message scratch (set on failure).
//
// Returns non-NULL on success. On failure returns NULL and writes a
// human-readable message into err_buf (if provided).
xg_tokenizer_info* xg_tokenizer_info_new(
    const char* const* vocab,
    int32_t vocab_size,
    xg_vocab_type vocab_type,
    const int32_t* stop_token_ids,
    int32_t stop_count,
    bool add_prefix_space,
    char* err_buf,
    size_t err_len);

void xg_tokenizer_info_free(xg_tokenizer_info* h);

// Build a Grammar from an EBNF/GBNF string.
xg_grammar* xg_grammar_from_ebnf(
    const char* ebnf,
    char* err_buf,
    size_t err_len);

// Build a Grammar from a JSON schema string.
xg_grammar* xg_grammar_from_json_schema(
    const char* schema,
    bool any_whitespace,
    bool strict_mode,
    char* err_buf,
    size_t err_len);

void xg_grammar_free(xg_grammar* h);

// Compile a Grammar against a TokenizerInfo. The returned CompiledGrammar
// owns its own reference to both inputs; the caller may free the inputs
// after this call.
xg_compiled_grammar* xg_compile_grammar(
    xg_tokenizer_info* tok,
    xg_grammar* g,
    char* err_buf,
    size_t err_len);

void xg_compiled_grammar_free(xg_compiled_grammar* h);

// Create a stateful matcher from a compiled grammar.
xg_matcher* xg_matcher_new(
    xg_compiled_grammar* cg,
    char* err_buf,
    size_t err_len);

void xg_matcher_free(xg_matcher* h);

// Required size (in int32 words) of the next-token bitmask buffer for a
// given vocab size. Caller pre-allocates that many int32_t entries.
int32_t xg_bitmask_size(int32_t vocab_size);

// Fill the bitmask for tokens that are valid at the current matcher
// state. Bit i corresponds to token id i; 1 = allowed, 0 = rejected.
//   bitmask     : pre-allocated int32 buffer of length xg_bitmask_size(vocab_size).
//   vocab_size  : vocab size; must match the TokenizerInfo used to compile.
// Returns 1 if the bitmask is meaningful (not all-allowed), 0 if every
// token is allowed (caller may skip the mask), -1 on error.
int xg_matcher_fill_next_token_bitmask(
    xg_matcher* m,
    int32_t* bitmask,
    int32_t vocab_size,
    char* err_buf,
    size_t err_len);

// Feed a sampled token back into the matcher. Returns 1 on accept,
// 0 on reject (token violates the grammar), -1 on error.
int xg_matcher_accept_token(
    xg_matcher* m,
    int32_t token_id,
    char* err_buf,
    size_t err_len);

// Roll the matcher back by num_tokens steps.
int xg_matcher_rollback(
    xg_matcher* m,
    int32_t num_tokens,
    char* err_buf,
    size_t err_len);

// Reset to initial state.
void xg_matcher_reset(xg_matcher* m);

// State queries.
bool xg_matcher_is_terminated(xg_matcher* m);
bool xg_matcher_is_completed(xg_matcher* m);

#ifdef __cplusplus
}  // extern "C"
#endif

#endif  // OLLAMA_XGRAMMAR_C_H_
