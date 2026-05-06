# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build / Test / Lint

```bash
# Build
go build -o cs .

# Run all tests
go test ./...

# Run tests for a single package
go test ./session/tmux/...
go test ./config/...
go test ./app/...

# Run a single test
go test ./session/tmux/... -run TestName

# Lint
gofmt -w .
```

The `web/` directory is a Next.js documentation site:
```bash
cd web && npm run dev      # dev server
cd web && npm run build    # production build
cd web && npm run lint     # ESLint
```

## Architecture

This is a Go TUI application (Bubble Tea framework) that manages multiple AI coding agents simultaneously. Each agent runs in its own isolated environment.

**Core abstraction — `session.Instance`** (`session/instance.go`): An "instance" combines a **git worktree** (isolated branch) + a **tmux session** running an AI agent program (claude, codex, gemini, aider, etc.). Starting an instance creates both; pausing tears down the worktree but keeps the branch; killing destroys both.

**Layer stack:**
- `main.go` — Cobra CLI entry point. Parses flags (`--program`, `--autoyes`), validates we're in a git repo, delegates to `app.Run()`.
- `app/` — Bubble Tea TUI. The `home` model holds all UI state (list, menu, preview/diff panes, overlays). A background ticker polls each active instance's tmux pane for updates and auto-clicks "yes" prompts when AutoYes is enabled.
- `session/` — Instance lifecycle. `NewInstance()` creates an unstarted instance; `Start(true)` sets up the git worktree then launches tmux; `Pause()`/`Resume()` save/restore state; `Kill()` tears everything down.
- `session/tmux/` — Thin wrapper around tmux. `Start()` runs `tmux new-session -d -s <name> -c <workdir> <program>`, then polls for session existence with exponential backoff. `Attach()`/`Detach()` manage the PTY connection for interactive use. `CapturePaneContent()` reads pane output for the preview/diff UI.
- `session/git/` — Git worktree operations: create from HEAD or existing branch, commit, push, diff stats, branch search.
- `config/` — JSON config stored at `~/.claude-squad/config.json`. Supports named profiles (program presets), AutoYes mode, daemon poll interval, and branch prefix.
- `daemon/` — Background process that polls all sessions and auto-accepts prompts. Only used in `--autoyes` mode.
- `ui/` — Bubble Tea view components: session list, preview pane, diff pane, terminal pane, tabbed window, overlays (text input, branch picker, profile picker, confirmation modals).
- `cmd/` — `Executor` interface wraps `exec.Cmd` for testability.

## Environment Variables for Spawned Agents

When `ds` spawns an AI agent (e.g., `claude`), the agent inherits environment variables through Go's `exec.Command` default behavior — the tmux process started at `session/tmux/tmux.go:98` gets `os.Environ()` from the `cs` process, and passes it to the agent running inside tmux.

To use DeepSeek models, set these in your shell before running `ds`:

```bash
export ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic
export ANTHROPIC_AUTH_TOKEN=<your-deepseek-api-key>
export ANTHROPIC_MODEL=deepseek-v4-pro[1m]
export ANTHROPIC_DEFAULT_OPUS_MODEL=deepseek-v4-pro[1m]
export ANTHROPIC_DEFAULT_SONNET_MODEL=deepseek-v4-pro[1m]
export ANTHROPIC_DEFAULT_HAIKU_MODEL=deepseek-v4-flash
```

The same principle applies for other agents: `OPENAI_API_KEY` for Codex, etc.

## Key Patterns

- **PTY-based tmux interaction**: The code communicates with tmux sessions through PTY file descriptors, not tmux commands. Key presses are forwarded byte-by-byte to `ptmx`; pane output is captured via `tmux capture-pane` subprocess calls.
- **Instance state machine**: Instances transition through `Loading → Running → Ready` (and `Paused`). Status is derived from tmux pane content parsing — e.g., detecting the string `"No, and tell Claude what to do differently"` means claude is waiting for input.
- **Test isolation**: Tmux and git operations use dependency injection (`PtyFactory`, `Executor` interfaces) so tests can substitute mocks.
- **Background I/O**: Expensive operations (git diff, tmux pane capture) run in goroutines and communicate results back to the Bubble Tea event loop via tea.Msg types.
