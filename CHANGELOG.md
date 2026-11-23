# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

No unreleased changes yet.

## [0.4.1] - 2026-11-24

A product-tier rename and an offline packaging correction. Runtime profiles
become capability tiers (`lite` / `pro` / `custom`), `lite` becomes the default
for fresh installs, and the full offline archive finally ships the model the
full-feature tier actually needs.

### Added

- **Offline `pro` weights in the full archive.** Release archives now bundle
  `intfloat/multilingual-e5-small` (revision
  `614241f622f53c4eeff9890bdc4f31cfecc418b3`) under
  `model/rembed/intfloat--multilingual-e5-small/`, plus the legacy aikit
  cross-encoder. kbcli auto-detects the bundled rembed cache next to the
  executable and points `REMBED_CACHE` at it, so `pro` indexing and querying
  work with zero network access. Both model sets are pinned and verified
  against `assets/model.sha256` during the release build.
- **Profile aliases and migration.** `local` and `multilingual` remain
  accepted aliases for `lite` and `pro` in config files, workspace overrides
  and `kbcli config set`, and are rewritten to the canonical names on save.
  A config file with no `profile` key is pinned to `custom` on load so an
  existing installation keeps its previous rerank settings.
- **Tests** for tier enforcement, alias canonicalization, legacy-config
  migration, offline bundle detection, incomplete/unsafe bundle rejection and
  the bundled model's cache configuration.

### Changed

- **Profiles are product tiers.** `profile` now accepts `lite`, `pro` and
  `custom`. `lite` replaces `local` and `pro` replaces `multilingual`. The
  names describe capability rather than implementation: `pro` also improves
  same-language conceptual recall, and it is deliberately not called `full`
  because `full` is the name of the offline release archive.
- **`lite` is the default for fresh installs.** It downloads nothing and
  starts with reranking disabled. `pro` still forces `rembed`, hybrid
  retrieval, a wider candidate pool and token-level MaxSim reranking;
  `custom` still keeps manual combinations.
- **Bilingual README guidance.** Both READMEs now document the tier names,
  the default, the upgrade path, the offline archive layout and the corrected
  settings catalog counts (52 fields in 8 groups).
- **Release workflow.** The model download step fetches both model sets in
  one pinned, checksum-verified step and stages them into every full archive.

### Fixed

- **The full archive shipped the wrong model.** It previously contained only
  the English-only `cross-encoder/ms-marco-MiniLM-L-6-v2` aikit reranker,
  which no productized tier uses by default, and omitted the multilingual
  embedding model that `pro` requires. Air-gapped `pro` installs were
  therefore impossible.
- **Stale README statements.** The settings catalog counts and the
  "reranking is on by default" claim had not been updated for the settings
  redesign and the new default tier.

### Upgrade notes

1. **Fresh installs start on `lite`.** No config change is needed; reranking
   is off and no model is downloaded. Run `kbcli config set profile pro` and
   `kbcli reindex` to switch to the full-feature tier.
2. **Existing configs are preserved.** A `config.yaml` without a `profile`
   key loads as `custom`. A config with `profile: local` or
   `profile: multilingual` resolves to `lite` or `pro` automatically. The
   canonical name is written on the next save.
3. **Switching tiers changes the embedding domain.** Moving between `lite`
   and `pro` requires one `kbcli reindex`; query-time knobs do not.
4. **Offline users should re-download the full archive.** Older full
   archives do not contain the `pro` weights. Alternatively set
   `REMBED_CACHE` to a directory containing
   `intfloat--multilingual-e5-small/`.

### Known limitations

- `pro` weights are ~450 MB, so a full archive is substantially larger than
  the normal binary. The same weights are duplicated in each per-platform
  archive.
- `custom` + `aikit` still downloads the English-only cross-encoder on first
  use unless the full archive is used; it is not the recommended tier for
  multilingual corpora.
- `embed.Dim` remains fixed at 384, so 768/1024-dimension models are unusable.

## [0.4.0] - 2026-11-24

A minor release that makes multilingual retrieval a first-class supported
workflow. It adds retrieval profiles with enforced settings, local token-level
reranking through the existing embedder, automatic KB language detection,
translation on quick-RAG routes, and a headless `kbcli ask --json` command for
agent and tool callers. The settings screen now places profile, embedding and
rerank controls on one page and moves LLM credentials to their own sidebar.

### Added

- **Retrieval profiles.** `profile` accepts `local`, `multilingual` and
  `custom`. Profiles force a supported embedder/rerank/translation stack and
  record each forced change with a reason. `custom` is the default and keeps
  pre-v0.4.0 behaviour.
- **Local MaxSim reranking.** `rerank_type: local_maxsim` uses token vectors
  from the active `rembed` model to rescore candidates. No second model, no
  external rerank service and no token vectors stored in the index.
- **Headless answers.** `kbcli ask <question>` answers without the TUI.
  `--json` emits a stable envelope with the answer, hits, profile, intent,
  detected query language, KB language, translation diagnostics and degraded
  status. `--explain` sends retrieval diagnostics to stderr.
- **Stable exit codes.** `0` success, `2` config error, `3` no index, `4` LLM
  failure and `5` retrieval failure.
- **Automatic KB language detection.** `kb_language: auto` now uses chunk-level
  majority voting instead of raw character counts, so identifier-heavy code
  samples no longer misclassify a Chinese corpus. Chunk metadata keys
  `language` and `lang` are honoured when present.
- **Query translation diagnostics.** `CondenseMeta` and `AnswerMeta` expose
  query language, KB language and translated search query. The TUI and
  headless JSON both surface them.
- **Tests.** New coverage for profile enforcement, local MaxSim reranking,
  quick-RAG translation, corpus language voting and headless command wiring.

### Changed

- **Quick RAG no longer skips translation.** It still skips condensing and
  query transforms, but cross-language alignment now runs for simple
  definitional questions.
- **Reranking uses the translated query.** After translation, candidates are
  reranked with the same language used for evidence search, avoiding a return
  to the original query-language bias.
- **Settings layout.** The first sidebar is now `Profile & Models`. It shows
  the effective profile summary and policy notes. A separate `LLM` sidebar
  holds `chat_provider`, `api_key`, `base_url` and `model`.
- **Bilingual README guidance.** Both READMEs document profile selection,
  headless usage and warm latency/recall trade-offs between `local` and
  `multilingual`.

### Fixed

- English queries against Chinese knowledge bases no longer default to an
  unrelated high-score PDF for definition-style questions. In the measured
  corpus, Chinese and English variants of "system design" now return the same
  top five chunks.
- Mixed-language queries such as `what's 系统设计` are detected as English,
  translated to the KB language and reranked consistently.
- Policy-forced settings are re-applied after workspace overrides are merged,
  so a workspace cannot silently break a selected profile.

### Upgrade notes

- No index invalidation is required for existing indexes. Switching from
  `local` to `multilingual` changes the embedder and therefore requires
  `kbcli reindex` once.
- Existing configurations without `profile` keep `custom` behaviour. Set
  `profile: multilingual` explicitly to opt into the new enforced stack.
- `kbcli config keys` now reports 52 fields in 8 groups.

## [0.3.1] - 2026-11-22

A patch release that corrects the neural embedder's retrieval-domain contract
and enables hybrid retrieval by default. It also adds a dependency-free
evaluation command and machine-readable search output. **`rembed` indexes are
invalidated**: run `kbcli reindex` once after upgrading. Indexes built with the
default `local` embedder are unchanged.

### Fixed

- **Asymmetric e5 prefixes.** `rembed` previously prepended `"query: "` to
  both queries and documents. The e5 family is trained with `"query: "` on
  queries and `"passage: "` on documents; using one prefix for both sides
  puts documents in the query representation space and silently degrades
  recall. `EmbedQuery` and `EmbedDocuments` now apply the correct side through
  an optional `QueryPrefixer` capability, and `CountForEmbedding` counts the
  document-side prefix.
- **Stale index detection.** `rembedVersion` now carries a domain version
  (`#v2`), so `loadStore` rejects indexes built with the old symmetric prefix
  instead of comparing incompatible vectors. `IndexSignature()` records
  `embed_domain` for neural providers only, and `IndexSignatureChanges` now
  reports every differing key, including derived keys such as `chunker` and
  `embed_domain` that have no editable setting.
- **Neural model memory.** `rembed.Load` now uses `WithInt8()` weight-only
  quantization, cutting weight traffic about 4x with cosine >= 0.999 against
  fp32. rembed falls back to fp32 on CPUs without AVX2+FMA. No new dependency
  and no change to binary size.

### Changed

- **Hybrid retrieval is the default.** `retrieval_method` now defaults to
  `hybrid`, which fuses dense vectors with BM25 by weighted RRF before
  reranking. `vector` and `bm25` remain available for single-channel
  workloads. This is a query-time change: no re-index is required for
  `local` indexes, and the BM25 index is still built lazily on first use.

### Added

- **`kbcli eval`** runs a deterministic self-retrieval smoke test over indexed
  chunks. It reports `recall@1`, `recall@k`, MRR and p50/p95 query latency,
  and accepts `--samples`, `--top-k` and `--json`. It needs no API key, no
  labelled data and no model download, so it can run in CI. Queries are
  generated from chunk bodies, so recall is optimistic by design: use it to
  detect regressions between configurations, not to claim answer quality.
- **`kbcli search --json`** prints ranked hits as a stable JSON array with
  `rank`, `score`, `source`, `heading`, `body` and `id`, for tool consumers.
- **Tests** for the prefix capability, the rembed domain version and the
  store's end-to-end query/document prefix wiring.

### Upgrade notes

1. **Users of `embedder: rembed` must rebuild once.** Run `kbcli reindex` in
   the workspace, or type `:reindex` inside the TUI. The loader rejects an old
   `.kbvec` with a clear message. The old file stays on disk and is ignored.
2. **Users of the default `local` embedder need no action.** Its vectors,
   version (`local-v1`) and build signature are unchanged.
3. **Review the retrieval default.** Workloads tuned for `vector` can restore
   the previous behaviour with
   `kbcli config set retrieval_method vector`.
4. **Establish a baseline.** Run `kbcli eval --samples 50` before and after
   changing retrieval settings so recall changes are measurable.

### Known limitations

- The default `local` embedder is still a zero-dependency hashing model. It
  remains the largest recall bottleneck; `embedder: rembed` is opt-in.
- `rembed` int8 weight quantization can fall back to fp32 on CPUs without
  AVX2+FMA. Vectors then differ slightly across machines, so a `.kbvec`
  built on one host should be rebuilt on a host with a different CPU feature
  set when exact reproducibility matters.
- The vector width is still fixed at 384. Models with 768 or 1024 dimensions
  are not usable yet.
- `chunk_size: 4000` is a rune cap, while the default neural model accepts
  about 512 tokens. `capPassagesByTokens` hard-splits over-long sections, so
  the rune cap has little effect for `rembed` users.
- `Chunk.Metadata` is still never populated, and retrieval has no metadata
  filter. `kbcli eval` is a self-retrieval smoke test, not a question-answer
  benchmark.
- Retrieval still uses an exact brute-force scan with a full sort. An ANN
  index and vector quantization are not implemented.
- `read_file` can read any text file inside the workspace, including `.env`
  style secrets. Its results are not numbered with `[n]`.
- The boot and reindex full-screen modals still accept only `ctrl+c`.
- Windows runtime behavior has not been verified on real hardware.

## [0.3.0] - 2026-11-22

The second feature release. It adds a first-run API key wizard, per-extension
chunking, a production-grade retrieval pipeline, cancellable streaming with a
per-tab spinner, read-only workspace tools and a rewritten grounded system
prompt. It also upgrades the agent SDK. **Existing `.kbvec` indexes are
invalidated**: run `kbcli reindex` once after upgrading.

### Added

- **First-run API key wizard.** A full-screen TUI wizard collects
  `chat_provider`, a masked `api_key`, `base_url` and `model` before chat.
  Saving validates the config, runs a 5-second HTTP probe against the endpoint
  (any HTTP response counts as reachable), persists `~/.kbcli/config.yaml`, and
  rebuilds the agent, retriever and session so the new key takes effect without
  a restart. `ctrl+s` skips it and records `setup_done: true` for offline use.
  A failed probe only produces a warning; the config is still saved.
- **Per-extension chunking.** `internal/ingest` now uses a `Strategy` interface
  and a `Registry` that selects a splitter by file extension: Markdown headings
  for `.md` / `.markdown` / `.mdx`, plain-text recursive split for `.txt` /
  `.text`, row groups for `.csv`, HTML headings for `.html` / `.htm`, pages for
  `.pdf`, and converted Markdown sections for `.docx`. `chunk_strategy: fixed`
  restores the legacy single-size splitter for every format.
- **New settings.** `section_size` (default 1000 runes) caps one structured
  section, `chunk_strategy` (`auto` | `fixed`) selects the chunker, and
  `candidate_k` (default 40) widens the pre-rerank candidate pool. `setup_done`
  is persisted but hidden from the settings catalog. The catalog is now 49
  fields in 7 groups, 23 of them numeric.
- **Production RAG fixes.** Retrieval fetches `candidate_k` candidates, recalls
  the original question alongside a condensed rewrite with weighted RRF,
  normalizes and re-sorts reranker scores, and applies `min_score` after
  reranking. A shared citation ledger numbers the grounded prompt and
  `kb_search` results consistently, and each citation is a separate budget part
  so trimming never cuts an `[n]` marker. Empty or failed retrieval now returns
  an explicit refusal; `AnswerMeta.Degraded` / `Reason` surface degradation in
  the TUI status line.
- **Read-only workspace tools.** The ReAct agent now receives `ls`,
  `read_file`, `glob` and `grep` alongside `kb_search`. All four are scoped to
  `Store.Root()`: absolute paths, `..` traversals and symlinks escaping the
  workspace are rejected, binary files are refused, and results are capped at
  200 entries, 64 KiB per read and 100 grep matches. No write tool is exposed.
- **Cancellable streaming.** One cancellable context now spans the whole
  post-`enter` pipeline (intent classification, retrieval, reranking and
  generation). `esc` cancels it; a per-request generation drops late delta,
  reset and done messages so a cancelled answer never reappears. The thinking
  indicator is an animated braille spinner with an `esc to cancel` hint, and
  tool-round preambles are cleared from the live transcript via `StreamHooks`.
  `chat.cancelled` and the revised `chat.thinking` are bilingual.
- **Streaming API surface.** `agent.StreamHooks` / `HookedStreamAnswerer`
  expose `OnReset` so a ReAct tool-round preamble never reaches the answer.
  `askWithContext` builds a command from a model-owned context, and
  `answerResetMsg` / `thinkingTickMsg` / `spinRunning` drive the live
  transcript, the shared spinner chain and stale-message filtering in the TUI.
- **Per-tab spinners.** One shared spinner tick chain is driven by
  `anyWindowBusy()` instead of the active window's state, so switching to an
  idle tab never freezes a background request's animation. `tabBarView`
  reserves one fixed cell per tab for the rotating frame (blank when idle),
  keeping the bar width constant when requests start or finish. `spinRunning`
  prevents duplicate chains when several tabs generate at once.
- **Tests and i18n.** New coverage for the wizard, runner rebuild, strategy
  registry, candidate widening, post-rerank threshold, context expansion,
  original-query recall, citation-safe trimming, short-question routing,
  index invalidation, filesystem-tool path confinement, tool-round filtering,
  cancellation, stale-message dropping and spinner frames. All new UI strings
  are bilingual with parity tests.

### Changed

- `chunk_size` now caps **plain-text chunks only**; structured formats are
  capped per section by `section_size`. `IndexSignature()` additionally records
  `section_size`, `chunk_strategy` and the `per-ext-v1` chunker version.
- Short ASCII questions such as `why?` no longer route to `direct`; they reach
  the knowledge base instead of silently bypassing grounding.
- `bm25_top_k` is relabelled "BM25 candidate K"; `candidate_k` is now the broad
  query-time pool and does not invalidate the index.
- **Defaults raised.** `max_iterations` 8 → 50 so multi-step tool loops no
  longer trip `graph: recursion limit (8) exceeded`; `context_max_chunk_chars`
  800 → 1600 so each injected citation carries roughly twice the context
  (6 hits × 1600 runes stays inside the 16000-rune budget).
- **System prompts rewritten.** `SystemPrompt`, `SystemPromptZh` and the
  bilingual `prompt.system_*` strings no longer tell the model to recite
  evidence. They define instruction priority, treat retrieved text as
  untrusted, require synthesis across passages, describe the read-only tool
  policy, keep `[n]` citations, and specify gap handling. `systemPromptFor`
  selects the agent-level prompt from the configured language.
- Both READMEs document the wizard, the per-extension strategy table, the
  revised chunk-size semantics and the updated field counts.
- `esc` no longer quits the chat screen. While a request runs it cancels that
  request; while idle it is a no-op, so an accidental second `esc` after a
  cancel cannot exit the app. `q`, `:q` and `ctrl+c` remain the quit keys.
- **Dependency upgrade.** `github.com/projanvil/langchain-golang` moves
  `v0.14.1` → `v0.14.3`. v0.14.2 is an additive Python-parity batch (new
  retrievers, compressors, formatters and output parsers) and v0.14.3 fixes
  its CI; neither changes any API kbcli imports. `go mod tidy` promoted
  `github.com/muesli/termenv v0.16.0` from indirect to direct because
  `internal/tui/theme_quit_test.go` imports it.

### Fixed

- `context_expand` neighbours are no longer discarded by the final `take(k)`;
  primary hits are taken first, then neighbours are appended and capped.
- Reranker scores are normalized (`sigmoid` for `aikit` logits, clamp for
  cross-encoder relevance) and the hits are re-sorted, so reranking actually
  changes the order and `min_score` is comparable to the displayed score.
- A failed query transform now falls back to the original question, not the
  rewritten one.
- `Ask` no longer hides retrieval and LLM failures; degradation is reported
  instead of polluting the session with fallback text.
- Grounded prompt and `kb_search` no longer use two independent `[n]` numberings.
- **Streaming is no longer a broken half-path.** `streamAnswer` now keeps only
  the last model round that produced no tool calls (detected from
  `StreamModelEnd.ToolCalls` and `StreamToolStart`), so a ReAct preamble never
  leaks into the answer. An upstream `context.Canceled` terminal event no
  longer forces a redundant `Invoke` when final text is already available,
  while a real caller cancellation is propagated instead of resurrecting a
  discarded answer. A cancelled run never writes a fabricated fallback answer
  or an assistant turn into session memory.

### Upgrade notes

- Existing `.kbvec` indexes are **invalidated** by the new chunker version and
  signature fields. Run `kbcli reindex` (or `:reindex`) once after upgrading;
  no compatibility shim is provided.
- The first launch opens the API key wizard. Configure `api_key` beforehand or
  press `ctrl+s` to stay offline.

### Known limitations

- The default embedder is still a zero-dependency hashing model rather than a
  semantic vector model; it remains the largest recall bottleneck.
- Retrieval still expands to adjacent chunks of the same file, not to sibling
  files in the top hit's directory.
- Windows runtime behaviour is still unverified on real hardware.

## [0.2.0] - 2026-11-22

The first feature release after 0.1.1. Highlights: streaming answers, an
Emacs-style `ctrl+x` multi-window TUI with a tab bar, in-TUI boot and reindex
screens, intent routing with a local cross-encoder reranker on by default,
and Anthropic chat support. See the
[release notes](https://github.com/ProjAnvil/kbcli/releases/tag/v0.2.0) for
the full narrative and upgrade guide.

### Added

- **Streaming answers.** `agent.StreamAnswerer` adds `AskStream`, which
  consumes `StreamEvents` from the `langchain-golang` fork and forwards
  `model_delta.Text` deltas to the TUI as they arrive. Text renders
  incrementally, the busy spinner is hidden while streaming, and conversation
  history is written exactly once when the stream completes. A stream error
  or an empty stream fails open to the existing `Ask` path.
- **Multi-window chat.** `ctrl+x` is a window prefix following the Emacs `C-x`
  convention: `ctrl+x t` opens a window, `ctrl+x n` / `ctrl+x p` move between
  them, `ctrl+x 0`...`ctrl+x 8` jump to a numbered window, `ctrl+x x` closes
  the current window, and `ctrl+x w` opens a toast-style picker with up/down
  navigation, enter to switch and esc to cancel. The prefix times out after
  750 ms and keys pressed after it never reach the text input.
- **Browser-style tab bar.** One fixed row above the help line lists all
  windows and highlights the active one. New windows extend the bar to the
  right; the input row, tab bar and help row never shift.
- **Isolated per-window state.** Each window keeps its own transcript,
  evidence, input, viewport, focus, stream state, `agent.Answerer`, evidence
  sink and session memory (`root#window-N`). Background answers are routed
  back to the window that asked the question, not the visible one.
- **TUI boot screen.** First-run provisioning of the managed reranker model
  happens inside the TUI: ASCII wordmark, byte-level progress bar, phase and
  file labels, and an error view. Chat stays blocked until it completes.
- **`reindex` in-screen view.** Reindex reuses the boot-screen layout with a
  file-level progress bar, phase and current-file labels, and a persistent
  error view. Chat input stays blocked for the whole run.
- **`bubbles/key` bindings.** Chat bindings now use `key.Binding` and
  `key.Matches` with a `help.KeyMap` implementation; `ctrl+c` is a shared
  global quit binding available before the model exists.
- **Bilingual strings** for every boot, reindex, window, picker and key-help
  label in English and Simplified Chinese, with parity tests.
- **Tests** for the boot screen, reindex view, streaming (agent and TUI),
  multi-window answer routing, window picker, tab bar, prefix timeout and
  managed model provisioning.
- **Docs**: keyboard shortcuts and windows sections in both READMEs, plus
  updated boot, rerank and install instructions.

### Changed

- Reranking is on by default with the local `aikit` cross-encoder
  (`rerank_enabled: true`, `rerank_type: aikit`,
  `rerank_model: ms-marco-MiniLM-L-6-v2`). First run downloads five model
  files (~92 MB) into `~/.kbcli/model/` via the boot screen; setting
  `rerank_model_path` uses an external local model and skips it.
- Startup model provisioning moved from the CLI to the TUI. The CLI no longer
  writes progress lines to stderr before the alternate screen starts.
- Chunk defaults now follow LangChain (`chunk_size: 1024`,
  `chunk_overlap: 200`) and `rembed` input tokens are capped. Changing either
  chunk setting invalidates existing indexes and shows INDEX STALE.
- `chat_provider` selects `openai` (Chat Completions) or `anthropic` (Messages
  API), with a protocol-appropriate default `base_url` fallback for the
  official Anthropic API.
- Porto-derived intent routing (`off` / `binary` / `adaptive`) picks
  `direct`, `quick_rag` or `deep_rag` before retrieval; an unusable classifier
  falls back to keyword rules. Five new settings: `intent_mode`,
  `intent_provider`, `intent_api_key`, `intent_base_url`, `intent_model`.
- The TUI theme now follows the terminal background (OSC 11 / `COLORFGBG`);
  `:q` quits; `ctrl+]` toggles the evidence pane for full-width chat; chat
  messages wrap to the chat pane width.
- Markdown ingestion strips headings so they are not duplicated into chunk
  bodies; reindex runs as a blocking modal with progress.
- Full offline release archives (names containing `full`) bundle the binary
  and all five model files; each release also uploads `checksums.txt`.

### Fixed

- Long chat lines no longer disappear beneath the evidence pane; they wrap to
  the pane width.
- Opening a new window no longer leaves a frozen duplicate input box or
  pushes the tab bar down a row: `App.NewWindow` no longer writes to stderr
  while the alternate screen is active, and the bottom layout reserve is
  fixed at six rows.
- `TestMinScoreAndContextExpand` no longer depends on whether the reranker
  model is present locally: it disables the now-default reranker and compares
  the expanded chunk against its parent's score.

### Removed

- `internal/cli/progress.go` and its test. The console percentage progress
  printer is dead code now that the TUI owns model and reindex progress.

### Upgrade notes

- Existing `.kbvec` indexes are stale after the chunk-default change. Run
  `kbcli reindex` (or `:reindex` in the TUI) once after upgrading.
- The first launch downloads ~92 MB of reranker model files. Use a `full`
  release archive, or set `rerank_model_path` to a local model directory, to
  stay fully offline.
- Scripts that parsed the old stderr progress line will no longer see it;
  progress is rendered inside the TUI.

### Known limitations

- Windows runtime behaviour is still unverified on real hardware; only
  cross-compilation and static checks were performed.
- Retrieval expands to adjacent chunks of the same file (`context_expand`),
  not to sibling files in the top hit's directory. Directory-level synthesis
  is not implemented yet.

## [0.1.1] - 2026-11-22

### Changed

- The Go module path now matches the GitHub organisation casing
  (`github.com/ProjAnvil/kbcli`); `go install github.com/ProjAnvil/kbcli/cmd/kbcli@v0.1.1`
  is the documented remote install.
- `kbcli version` now reports the installed module version for binaries
  installed with `go install pkg@version`, while Makefile `-ldflags` injection
  still takes precedence.

### Added

- A GitHub Actions release workflow cross-compiles darwin/amd64, darwin/arm64,
  linux/amd64, linux/arm64 and windows/amd64 binaries and uploads them as
  release assets on every `v*` tag push.

## [0.1.0] - 2026-11-21

### Added

- Single-file `.kbvec` vector index with a magic header, embedder-domain
  isolation, and atomic writes; legacy `.kbcli.kbvec` files remain loadable.
- Zero-dependency in-process hashing embedder by default and an optional
  pure-Go `rembed` embedder; both are 384-dimensional.
- OpenAI-compatible Chat Completions backend with a deterministic extractive
  fallback, so chat and search work offline.
- Porto-derived retrieval pipeline: vector, BM25, weighted-RRF hybrid,
  reranking, query transforms (HyDE, multi-query, decomposition, step-back),
  query condensation, plus `min_score` and `context_expand`; every
  LLM-dependent stage fails open.
- Bilingual English / Simplified Chinese TUI with a two-pane chat view, a
  master-detail settings screen, and per-workspace session facts with history
  compaction.
- Global `~/.kbcli/config.yaml` plus sparse per-workspace `settings.json`
  overrides, with an INDEX STALE banner driven by a version-2 build signature.
- Cobra commands `index`, `reindex`, `update`, `config`, `settings`, `search`,
  `version`, and in-TUI commands `:reindex`, `:update`, `:settings`, `:stale`,
  `:clear`, `:help`, `:index`.
- All filesystem paths use Go standard-library helpers; the tree cross-compiles
  for Windows, macOS, and Linux. The Windows runtime has NOT been verified on
  real hardware.
- `:index` requires a two-step directory confirmation backed by a bounded
  read-only preflight (20,000 files / 12 levels) whose risk grading protects
  home and system roots.

### Changed

- Startup no longer auto-indexes, no longer walks parent directories, and
  performs no directory walk at all; indexing is strictly on demand.

---

This is the first public release, so there are no prior entries.
