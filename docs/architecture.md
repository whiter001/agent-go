# Architecture

## High-level flow

1. `main.go` forwards CLI arguments to `internal/app.Main`.
2. `internal/app` parses flags and subcommands, loads configuration, and routes the request.
3. `internal/config` merges defaults, config files, environment variables, and path normalization.
4. `internal/agent` runs the LLM loop, tracks messages, executes tools, and compacts history.
5. `internal/llm` adapts the shared schema to Anthropic-style or OpenAI-style HTTP APIs.
6. `internal/tools` implements file, shell, note, and durable-memory actions.
7. `internal/store` persists long-term memory and regenerates `MEMORY.md` / `USER.md` snapshots.
8. `internal/logging` writes structured run logs.
9. `internal/skills` discovers `SKILL.md` files and injects relevant context.

## Core packages

### `internal/schema`

Shared message, tool, response, and token-usage types used across the agent.

### `internal/utils`

Formatting helpers such as ANSI stripping, display-width calculation, whitespace normalization, and middle truncation.

### `internal/agent`

The execution engine:

- appends the current workspace to the system prompt
- keeps conversation history and optional ephemeral context
- executes tool calls returned by the model
- records tool output and logging events
- summarizes older execution rounds when the token budget grows too large

### `internal/llm`

Supports two providers:

- `anthropic`
- `openai`

It also handles retry/backoff and request/response translation between the shared schema and provider-specific payloads.

### `internal/tools`

Current tool set:

- `read_file`, `write_file`, `edit_file`
- `bash`, `bash_output`, `bash_kill`
- `record_note`, `recall_notes`
- `remember`, `remember_user`, `search_memory`

### `internal/store`

Persistent memory is stored under `~/.agent-go/` by default:

- `memory.json` — durable entries
- `MEMORY.md` — human-readable memory snapshot
- `USER.md` — user profile snapshot

### `internal/logging`

Run logs are stored as newline-delimited JSON in `~/.agent-go/log/agent_run_*.log`.

### `internal/skills`

Skill discovery walks configured directories, finds `SKILL.md` files, parses lightweight frontmatter metadata, scores them against the current query, and builds section-aware turn-context snippets from the most relevant skills. Successful runs can also emit draft autoskills for later reuse.

## Current bootstrap note

The core agent runtime is already present, but the CLI bootstrap currently keeps prompt and interactive execution in placeholder mode. That makes the codebase easy to extend without pretending those flows are finished already.
