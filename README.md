## Fork notes — XGrammar grammar backend

This fork adds an optional [XGrammar](https://github.com/mlc-ai/xgrammar)
backend for constrained decoding (`format=<schema>` / `format="json"`)
in the new ollama engine. The default GBNF path is unchanged; the new
backend is gated by a build tag and an environment variable so vanilla
builds are unaffected.

### Build prerequisites

- Go 1.26+
- CMake 3.18+
- C/C++ compiler (Xcode Command Line Tools on macOS, GCC/Clang on Linux)

### Install from source

Clone this fork with submodules and check out the integration branch:

```shell
git clone --recurse-submodules -b xgrammar-integration https://github.com/ho4040/ollama.git
cd ollama
```

Build the xgrammar static archive once, then build the ollama binary
with the build tag:

```shell
cmake -S llama/xgrammar -B llama/xgrammar/build -DCMAKE_BUILD_TYPE=Release
cmake --build llama/xgrammar/build -j
go build -tags xgrammar -o ollama .
```

`./ollama --version` should now run from this directory. See
[`docs/development.md`](./docs/development.md) for the upstream
prerequisite list (Vulkan, ROCm, etc.) if you need a non-default
backend.

### Replace an existing Homebrew install (macOS)

If you already have `brew install ollama` on the machine, the
fork binary needs to take over the symlink in `PATH`:

```shell
brew services stop ollama 2>/dev/null
brew unlink ollama                                # keep the cellar; remove the symlink
ln -sf "$PWD/ollama" /opt/homebrew/bin/ollama     # PATH now resolves to the fork build
```

To remove the brew copy entirely after the fork is running:

```shell
brew uninstall ollama
```

`~/.ollama/models` is not touched by either step, so previously pulled
models are preserved.

### Run as a daemon (macOS LaunchAgent)

Save the following as
`~/Library/LaunchAgents/com.user.ollama-xgrammar.plist`, replacing
`USERNAME` with the output of `whoami`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.user.ollama-xgrammar</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/USERNAME/work/ollama/ollama</string>
        <string>serve</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>OLLAMA_NEW_ENGINE</key><string>true</string>
        <key>OLLAMA_GRAMMAR_BACKEND</key><string>xgrammar</string>
    </dict>
    <key>WorkingDirectory</key>
    <string>/Users/USERNAME/work/ollama</string>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key>
    <string>/Users/USERNAME/Library/Logs/ollama-xgrammar.log</string>
    <key>StandardErrorPath</key>
    <string>/Users/USERNAME/Library/Logs/ollama-xgrammar.log</string>
</dict>
</plist>
```

Load and start it:

```shell
launchctl load ~/Library/LaunchAgents/com.user.ollama-xgrammar.plist
```

The agent restarts the binary on crash and on login. After a rebuild,
restart it without re-loading the plist:

```shell
launchctl kickstart -k gui/$(id -u)/com.user.ollama-xgrammar
```

### Run as a daemon (Linux systemd, user unit)

Save as `~/.config/systemd/user/ollama-xgrammar.service`:

```ini
[Unit]
Description=Ollama with xgrammar backend
After=network-online.target

[Service]
ExecStart=%h/work/ollama/ollama serve
WorkingDirectory=%h/work/ollama
Environment=OLLAMA_NEW_ENGINE=true
Environment=OLLAMA_GRAMMAR_BACKEND=xgrammar
Restart=on-failure

[Install]
WantedBy=default.target
```

Enable and start:

```shell
systemctl --user daemon-reload
systemctl --user enable --now ollama-xgrammar
```

This Linux path is supported by the build but is not part of the
local validation matrix below; it has only been smoke-tested.

### Configuration

Three environment variables drive the new code paths:

| Variable | Values | Effect |
|---|---|---|
| `OLLAMA_NEW_ENGINE` | `true` / unset | required for the xgrammar path; the legacy llamarunner engine is unaffected |
| `OLLAMA_GRAMMAR_BACKEND` | `xgrammar` / unset | selects the backend; default is GBNF |
| `OLLAMA_XGRAMMAR_VOCAB_TYPE` | `raw` / `byte_fallback` / `byte_level` / unset | overrides the auto-detected vocab encoding; useful only if the heuristic mispredicts for a custom tokenizer |

If xgrammar fails to initialise for a specific request, the
dispatcher logs a warning (`xgrammar backend init failed, falling
back to GBNF`) and serves the request through the legacy backend.

### Update

```shell
cd ollama
git pull --recurse-submodules
cmake --build llama/xgrammar/build -j         # rebuild xgrammar lib if upstream changed
go build -tags xgrammar -o ollama .
launchctl kickstart -k gui/$(id -u)/com.user.ollama-xgrammar  # macOS
# or
systemctl --user restart ollama-xgrammar                       # Linux
```

### Verify

```shell
ollama pull llama3.2:3b
curl -s http://127.0.0.1:11434/api/chat -d '{
  "model": "llama3.2:3b",
  "stream": false,
  "think": false,
  "format": {"type":"object","required":["name","age"],"properties":{"name":{"type":"string"},"age":{"type":"integer"}}},
  "messages": [{"role":"user","content":"Generate JSON for a fictional person."}]
}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["message"]["content"])'
```

The server log prints `xgrammar backend active mode=json_schema` for
the first request that loads the grammar; subsequent requests reuse
the matcher.

### Validation

The tables below are produced by `gemma4:26b` (a representative larger
local model) against the 12 hand-picked schemas plus 4 non-English-key
schemas described in the test plan, with `think=false` and the same
seed for both backends.

#### Per-schema results

| Schema | GBNF | xgrammar | Notes |
|---|---|---|---|
| `custom_korean_keys` | pass (1.0s) | pass (1.0s) | Hangul keys: 이름, 나이, 취미 |
| `custom_chinese_keys` | pass (0.9s) | pass (1.0s) | Hanzi keys: 姓名, 年龄, 城市 |
| `custom_japanese_keys` | pass (1.2s) | pass (1.3s) | Kana/Kanji keys: 名前, 年齢, 趣味 |
| `custom_mixed_unicode_values` | pass (2.6s) | pass (2.7s) | Korean values, mixed-language tags |
| `custom_flat` | pass (1.2s) | pass (1.2s) | name/age/tags |
| `custom_long_text` | pass (1.9s) | pass (1.9s) | multi-sentence string field |
| `custom_deep` | pass (1.4s) | pass (1.4s) | five levels of nesting |
| `Glaiveai2K/analyze_health_data` | pass (3.0s) | pass (3.0s) | function-call style |
| `Glaiveai2K/analyze_social_media_mentions` | pass (1.8s) | pass (1.8s) | function-call style |
| `Github_easy/o10009` | **err** (server) | fail (maxLength) | server rejects schema before sampling |
| `Github_easy/o10010` | **err** (server) | fail (maxLength) | server rejects schema before sampling |
| `Github_medium/o1` | **err** (sampler) | **pass** (4.4s) | only xgrammar accepts this schema |
| `Github_medium/o10078` | pass (4.3s) | pass (4.2s) | |
| `Github_hard/o10293` | fail (rating) | fail (rating) | required field omitted by model |
| `Github_hard/o10296` | fail (rating) | fail (rating) | required field omitted by model |
| `Github_ultra/o10335` | fail (truncated) | fail (truncated) | 140 KB schema; output exceeds 4096 tokens |

`err` = HTTP 500 from the server: the request never reaches the
sampler. `fail` = sampler ran but the response did not validate
against the schema.

#### Backend comparison summary

| | GBNF | xgrammar |
|---|---|---|
| pass | 10 / 16 | 11 / 16 |
| fail | 3 / 16 | 5 / 16 |
| err  | 3 / 16 | 0 / 16 |

The 3 `err` cells under GBNF (`Github_easy/o10009`, `o10010`,
`Github_medium/o1`) are schemas that `llama.SchemaToGrammar` cannot
express; the request fails before any sampling happens. xgrammar's
schema-direct path accepts all three, and one of them (`Github_medium/o1`)
becomes a passing run end-to-end. The other two reach the sampler but
the response still violates `maxLength`/`pattern` constraints, which
neither backend currently emits as grammar tokens.

The 4 non-English-key schemas pass on both backends, confirming that
UTF-8 multi-byte tokens are masked correctly through the cgo boundary.

#### Failure causes for xgrammar (5 / 16)

| Schema | Root cause |
|---|---|
| `Github_easy/o10009`, `o10010` | `maxLength` / `minLength` / `pattern` on string fields. xgrammar's JSON-Schema-to-grammar conversion does not emit length or pattern constraints, so the model is free to write a longer hex string. GBNF has the same limitation but the server rejects the schema earlier. |
| `Github_hard/o10293`, `o10296` | The `rating` field is `required` deep inside `final_results.<player>`. The model substitutes `rating_delta` and the grammar matcher allows the closing brace because it cannot foresee the missing key from the partial state. |
| `Github_ultra/o10335` | 140 KB schema; `gemma4:26b` exhausts `num_predict=4096` before completing the response. Pure model/budget limit, not backend-specific. |

### Known limitations

- The xgrammar wrapper assumes an in-tree CMake build directory at
  `llama/xgrammar/build`; out-of-tree build dirs are not currently
  supported (cgo `LDFLAGS` use `${SRCDIR}/build` directly).
- Vocab-type detection is a heuristic over the decoded vocabulary
  (looks for `<0xHH>` byte tokens or GPT-2-style `Ġ`). Tokenizers that
  use neither marker default to `raw`; override with
  `OLLAMA_XGRAMMAR_VOCAB_TYPE` if the heuristic mispredicts.
- xgrammar `Rollback` is wired through to the C ABI but not used by
  the new engine yet; it is exposed for future jump-forward decoding
  work.

### Syncing with upstream

`origin` points at `ollama/ollama` and `fork` at this repository.
To pick up upstream commits and rebase the integration branch on top:

```shell
git fetch origin
git rebase origin/main                          # resolve conflicts
git push fork xgrammar-integration --force-with-lease
```

`--force-with-lease` refuses to overwrite the fork branch if someone
else has pushed to it in the meantime, which is safer than `--force`.
