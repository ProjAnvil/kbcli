# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
