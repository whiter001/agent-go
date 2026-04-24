# agent-go

`agent-go` is a Go-based multi-turn agent CLI with configuration loading, LLM client adapters, file and shell tools, persistent memory, logging, and skill discovery.

## Documentation

- [Project docs](docs/README.md)
- [Usage](docs/usage.md)
- [Architecture](docs/architecture.md)
- [Configuration](docs/configuration.md)

## Local setup

- Copy `.env.example` to `.env` and fill in your local API credentials before running the agent.

## Scripts

- `./scripts/check.ps1` — format check, `go vet`, tests, and build validation
- `./scripts/fmt.ps1` — format all Go packages
- `./scripts/build.ps1` — build `bin/agent-go.exe`
