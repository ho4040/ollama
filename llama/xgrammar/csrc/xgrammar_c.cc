// Copyright (c) 2026 The Ollama Authors
//
// C ABI implementation. Wraps xgrammar's C++ classes behind opaque
// handles and translates C++ exceptions into return-code + error string.

#include "../xgrammar_c.h"

#include <dlpack/dlpack.h>
#include <xgrammar/compiler.h>
#include <xgrammar/grammar.h>
#include <xgrammar/matcher.h>
#include <xgrammar/tokenizer_info.h>

#include <cstring>
#include <exception>
#include <optional>
#include <string>
#include <vector>

namespace {

void set_err(char* err_buf, size_t err_len, const char* msg) {
  if (!err_buf || err_len == 0) return;
  std::strncpy(err_buf, msg, err_len - 1);
  err_buf[err_len - 1] = '\0';
}

// Catch-all helper to map C++ exceptions to err_buf without leaking through
// the C ABI. T must be a pointer type; on exception returns nullptr.
template <typename Fn>
auto guarded(char* err_buf, size_t err_len, Fn&& fn)
    -> decltype(fn()) {
  try {
    return fn();
  } catch (const std::exception& e) {
    set_err(err_buf, err_len, e.what());
    return decltype(fn()){};
  } catch (...) {
    set_err(err_buf, err_len, "unknown C++ exception in xgrammar");
    return decltype(fn()){};
  }
}

}  // namespace

struct xg_tokenizer_info {
  xgrammar::TokenizerInfo info;
};

struct xg_grammar {
  xgrammar::Grammar grammar;
};

struct xg_compiled_grammar {
  xgrammar::CompiledGrammar compiled;
};

struct xg_matcher {
  xgrammar::GrammarMatcher matcher;
  int32_t vocab_size;  // cached from TokenizerInfo for bitmask shape checks.
};

extern "C" {

xg_tokenizer_info* xg_tokenizer_info_new(
    const char* const* vocab,
    int32_t vocab_size,
    xg_vocab_type vocab_type,
    const int32_t* stop_token_ids,
    int32_t stop_count,
    bool add_prefix_space,
    char* err_buf,
    size_t err_len) {
  return guarded(err_buf, err_len, [&]() -> xg_tokenizer_info* {
    if (vocab_size < 0 || (vocab == nullptr && vocab_size > 0)) {
      set_err(err_buf, err_len, "invalid vocab argument");
      return nullptr;
    }
    std::vector<std::string> encoded;
    encoded.reserve(static_cast<size_t>(vocab_size));
    for (int32_t i = 0; i < vocab_size; ++i) {
      encoded.emplace_back(vocab[i] ? vocab[i] : "");
    }

    std::optional<std::vector<int32_t>> stops;
    if (stop_token_ids != nullptr && stop_count > 0) {
      stops.emplace(stop_token_ids, stop_token_ids + stop_count);
    }

    xgrammar::VocabType vt;
    switch (vocab_type) {
      case XG_VOCAB_BYTE_FALLBACK:
        vt = xgrammar::VocabType::BYTE_FALLBACK;
        break;
      case XG_VOCAB_BYTE_LEVEL:
        vt = xgrammar::VocabType::BYTE_LEVEL;
        break;
      case XG_VOCAB_RAW:
      default:
        vt = xgrammar::VocabType::RAW;
        break;
    }

    auto* h = new xg_tokenizer_info{
        xgrammar::TokenizerInfo(
            encoded,
            vt,
            std::optional<int>{vocab_size},
            stops,
            add_prefix_space)};
    return h;
  });
}

void xg_tokenizer_info_free(xg_tokenizer_info* h) { delete h; }

xg_grammar* xg_grammar_from_ebnf(
    const char* ebnf,
    char* err_buf,
    size_t err_len) {
  return guarded(err_buf, err_len, [&]() -> xg_grammar* {
    if (!ebnf) {
      set_err(err_buf, err_len, "ebnf string is null");
      return nullptr;
    }
    return new xg_grammar{xgrammar::Grammar::FromEBNF(ebnf)};
  });
}

xg_grammar* xg_grammar_from_json_schema(
    const char* schema,
    bool any_whitespace,
    bool strict_mode,
    char* err_buf,
    size_t err_len) {
  return guarded(err_buf, err_len, [&]() -> xg_grammar* {
    if (!schema) {
      set_err(err_buf, err_len, "schema string is null");
      return nullptr;
    }
    return new xg_grammar{xgrammar::Grammar::FromJSONSchema(
        schema,
        any_whitespace,
        std::nullopt,  // indent
        std::nullopt,  // separators
        strict_mode,
        std::nullopt,  // max_whitespace_cnt
        false)};       // print_converted_ebnf
  });
}

void xg_grammar_free(xg_grammar* h) { delete h; }

xg_compiled_grammar* xg_compile_grammar(
    xg_tokenizer_info* tok,
    xg_grammar* g,
    char* err_buf,
    size_t err_len) {
  return guarded(err_buf, err_len, [&]() -> xg_compiled_grammar* {
    if (!tok || !g) {
      set_err(err_buf, err_len, "tokenizer or grammar handle is null");
      return nullptr;
    }
    xgrammar::GrammarCompiler compiler(tok->info);
    return new xg_compiled_grammar{compiler.CompileGrammar(g->grammar)};
  });
}

void xg_compiled_grammar_free(xg_compiled_grammar* h) { delete h; }

xg_matcher* xg_matcher_new(
    xg_compiled_grammar* cg,
    char* err_buf,
    size_t err_len) {
  return guarded(err_buf, err_len, [&]() -> xg_matcher* {
    if (!cg) {
      set_err(err_buf, err_len, "compiled grammar handle is null");
      return nullptr;
    }
    int vocab = cg->compiled.GetTokenizerInfo().GetVocabSize();
    return new xg_matcher{
        xgrammar::GrammarMatcher(cg->compiled), vocab};
  });
}

void xg_matcher_free(xg_matcher* h) { delete h; }

int32_t xg_bitmask_size(int32_t vocab_size) {
  return xgrammar::GetBitmaskSize(vocab_size);
}

int xg_matcher_fill_next_token_bitmask(
    xg_matcher* m,
    int32_t* bitmask,
    int32_t vocab_size,
    char* err_buf,
    size_t err_len) {
  if (!m || !bitmask) {
    set_err(err_buf, err_len, "matcher or bitmask is null");
    return -1;
  }
  if (vocab_size != m->vocab_size) {
    set_err(err_buf, err_len, "vocab_size mismatches compiled grammar");
    return -1;
  }
  try {
    DLTensor t{};
    t.data = bitmask;
    t.device = DLDevice{kDLCPU, 0};
    t.ndim = 1;
    t.dtype = xgrammar::GetBitmaskDLType();
    int64_t shape = xgrammar::GetBitmaskSize(vocab_size);
    t.shape = &shape;
    t.strides = nullptr;
    t.byte_offset = 0;
    bool need_apply = m->matcher.FillNextTokenBitmask(&t);
    return need_apply ? 1 : 0;
  } catch (const std::exception& e) {
    set_err(err_buf, err_len, e.what());
    return -1;
  } catch (...) {
    set_err(err_buf, err_len, "unknown C++ exception in FillNextTokenBitmask");
    return -1;
  }
}

int xg_matcher_accept_token(
    xg_matcher* m,
    int32_t token_id,
    char* err_buf,
    size_t err_len) {
  if (!m) {
    set_err(err_buf, err_len, "matcher is null");
    return -1;
  }
  try {
    return m->matcher.AcceptToken(token_id) ? 1 : 0;
  } catch (const std::exception& e) {
    set_err(err_buf, err_len, e.what());
    return -1;
  } catch (...) {
    set_err(err_buf, err_len, "unknown C++ exception in AcceptToken");
    return -1;
  }
}

int xg_matcher_rollback(
    xg_matcher* m,
    int32_t num_tokens,
    char* err_buf,
    size_t err_len) {
  if (!m) {
    set_err(err_buf, err_len, "matcher is null");
    return -1;
  }
  try {
    m->matcher.Rollback(num_tokens);
    return 0;
  } catch (const std::exception& e) {
    set_err(err_buf, err_len, e.what());
    return -1;
  } catch (...) {
    set_err(err_buf, err_len, "unknown C++ exception in Rollback");
    return -1;
  }
}

void xg_matcher_reset(xg_matcher* m) {
  if (!m) return;
  try {
    m->matcher.Reset();
  } catch (...) {
    // Reset() is not documented as throwing; swallow defensively.
  }
}

bool xg_matcher_is_terminated(xg_matcher* m) {
  if (!m) return false;
  try {
    return m->matcher.IsTerminated();
  } catch (...) {
    return false;
  }
}

bool xg_matcher_is_completed(xg_matcher* m) {
  if (!m) return false;
  try {
    return m->matcher.IsCompleted();
  } catch (...) {
    return false;
  }
}

}  // extern "C"
