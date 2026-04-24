# Configuration

## Resolution order

`internal/config.Load()` starts with defaults, then applies the first matching config file, then environment variables, and finally path normalization.

The file search order is:

1. `AGENT_GO_CONFIG` if it points to an existing file
2. Current working directory:
   - `config.json`
   - `config.yaml`
   - `config.yml`
   - `agent-go.json`
   - `agent-go.yaml`
   - `agent-go.yml`
   - `config/config.json`
   - `config/config.yaml`
   - `config/config.yml`
3. Home directory:
   - `~/.agent-go/config.json`
   - `~/.agent-go/config.yaml`
   - `~/.agent-go/config.yml`

## Default values

| Setting              | Default                    |
| -------------------- | -------------------------- |
| API base             | `https://api.minimaxi.com` |
| Model                | `MiniMax-M2.7`             |
| Provider             | `anthropic`                |
| Max agent steps      | `100`                      |
| Workspace directory  | `./workspace`              |
| System prompt path   | `system_prompt.md`         |
| Skills directory     | `./skills`                 |
| Auto skill directory | `~/.agent-go/skills`       |
| Auto skills limit    | `2`                        |
| MCP enabled          | `false`                    |

Tool toggles default to enabled:

- file tools
- bash
- note
- persistent memory
- skills
- auto skill discovery
- auto skill creation

Retry defaults are also enabled:

- max retries: `3`
- initial delay: `1s`
- max delay: `60s`
- exponential base: `2.0`

## Environment variables

### Config file override

- `AGENT_GO_CONFIG`

### LLM settings

- `AGENT_GO_API_KEY`, `MINI_AGENT_API_KEY`, `API_KEY`
- `AGENT_GO_API_BASE`, `MINI_AGENT_API_BASE`, `API_BASE`
- `AGENT_GO_MODEL`, `MINI_AGENT_MODEL`, `MODEL`
- `AGENT_GO_PROVIDER`, `MINI_AGENT_PROVIDER`, `PROVIDER`

For local development, copy `.env.example` to `.env` and fill in the values you need.

### Agent settings

- `AGENT_GO_WORKSPACE_DIR`, `MINI_AGENT_WORKSPACE_DIR`, `WORKSPACE_DIR`
- `AGENT_GO_MAX_STEPS`, `MINI_AGENT_MAX_STEPS`, `MAX_STEPS`
- `AGENT_GO_SYSTEM_PROMPT_PATH`, `MINI_AGENT_SYSTEM_PROMPT_PATH`, `SYSTEM_PROMPT_PATH`

### Tool toggles

- `AGENT_GO_ENABLE_FILE_TOOLS`, `MINI_AGENT_ENABLE_FILE_TOOLS`
- `AGENT_GO_ENABLE_BASH`, `MINI_AGENT_ENABLE_BASH`
- `AGENT_GO_ENABLE_NOTE`, `MINI_AGENT_ENABLE_NOTE`
- `AGENT_GO_ENABLE_MEMORY`, `MINI_AGENT_ENABLE_MEMORY`
- `AGENT_GO_ENABLE_SKILLS`, `MINI_AGENT_ENABLE_SKILLS`
- `AGENT_GO_ENABLE_AUTO_SKILLS`, `MINI_AGENT_ENABLE_AUTO_SKILLS`
- `AGENT_GO_ENABLE_AUTO_SKILL_CREATION`, `MINI_AGENT_ENABLE_AUTO_SKILL_CREATION`

### Auto skill tuning

- `AGENT_GO_AUTO_SKILLS_LIMIT`, `MINI_AGENT_AUTO_SKILLS_LIMIT`
- `AGENT_GO_AUTO_SKILL_MIN_TOOL_CALLS`, `MINI_AGENT_AUTO_SKILL_MIN_TOOL_CALLS`

### Skill directories

- `AGENT_GO_SKILLS_DIR`, `MINI_AGENT_SKILLS_DIR`
- `AGENT_GO_AUTO_SKILL_DIR`, `MINI_AGENT_AUTO_SKILL_DIR`

## Autoskill behavior

- Auto skill discovery loads metadata from `SKILL.md` frontmatter when present.
- Mixed Chinese/English prompts are tokenized more aggressively to improve skill selection.
- Turn context uses section-aware excerpts rather than dumping large raw skill blocks.
- Skill selections are recorded in a local feedback store and successful/helpful history can improve future ranking.
- When `enable_auto_skill_creation` is enabled, successful runs can emit draft autoskills into `auto_skill_dir`.
- Draft generation is gated by `auto_skill_min_tool_calls` and skips duplicate traces with the same generated signature.
- Ranking feedback is stored in `~/.agent-go/skill-feedback.json` by default.

## MCP settings

The codebase also carries MCP configuration fields in config files:

- `tools.enable_mcp`
- `tools.mcp_config_path` (`mcp.json` by default)
- `tools.mcp.connect_timeout` (`10` seconds)
- `tools.mcp.execute_timeout` (`60` seconds)
- `tools.mcp.sse_read_timeout` (`120` seconds)

## Path behavior

- `~` expands to the current user home directory.
- Whitespace is trimmed before values are used.
- Paths are cleaned with `filepath.Clean`.
- Relative paths are resolved against the configured workspace where applicable.
