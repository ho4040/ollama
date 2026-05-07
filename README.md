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

Measured on `gemma4:26b` with `think=false` and the same seed for
both backends. The schema set is restricted to cases that exercise
the grammar layer itself; schemas whose outcome is governed by
library-level limitations shared by both backends (`maxLength`,
`pattern`, deeply nested `required`) or by output-token budget are
not part of this set.

#### Where the two backends differ

| Schema | GBNF | xgrammar |
|---|---|---|
| `Github_medium/o1` | **err** — `llama.SchemaToGrammar` cannot express this schema, the request returns HTTP 500 before any sampling | **pass** (4.4s) — `Grammar::FromJSONSchema` compiles the raw schema directly and the response validates |

This is the case the integration is designed to recover. Schemas
that ollama's GBNF generator cannot translate are forwarded to
xgrammar's schema-direct path, which has wider JSON-Schema coverage.

#### Where the two backends agree (regression check)

The remaining schemas pass on both backends; they are listed to
confirm the new code path does not regress existing requests.

| Schema | GBNF | xgrammar |
|---|---|---|
| `custom_korean_keys` (Hangul keys) | pass (1.0s) | pass (1.0s) |
| `custom_chinese_keys` (Hanzi keys) | pass (0.9s) | pass (1.0s) |
| `custom_japanese_keys` (Kana/Kanji keys) | pass (1.2s) | pass (1.3s) |
| `custom_mixed_unicode_values` | pass (2.6s) | pass (2.7s) |
| `custom_flat` | pass (1.2s) | pass (1.2s) |
| `custom_long_text` | pass (1.9s) | pass (1.9s) |
| `custom_deep` (5 levels) | pass (1.4s) | pass (1.4s) |
| `Glaiveai2K/analyze_health_data` | pass (3.0s) | pass (3.0s) |
| `Glaiveai2K/analyze_social_media_mentions` | pass (1.8s) | pass (1.8s) |
| `Github_medium/o10078` | pass (4.3s) | pass (4.2s) |

The four non-English-key entries also serve as a UTF-8 round-trip
check across the cgo boundary; the multi-byte sequences make it back
through the bitmask and decoder unchanged.

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
