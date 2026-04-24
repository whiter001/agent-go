# Project documentation

This directory collects the living documentation for `agent-go`.

## What to read first

- `usage.md` — current command-line entry points and runtime notes
- `architecture.md` — how the packages fit together
- `configuration.md` — config files, defaults, and environment variables

## Current implementation status

The bootstrap CLI in `internal/app` currently exposes the `help` and `log` flows, plus placeholder prompt and interactive runtime paths. The underlying agent engine, LLM client adapters, tool implementations, persistent memory store, logging, and skill loader are already implemented under `internal/`.

That split is intentional: these docs describe both the stable behavior and the pieces that are ready for the next wiring step.

If you are extending the project, start with `architecture.md` and then read `configuration.md`.
