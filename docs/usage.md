# Usage

## Entry points

- `agent-go --version` — print the current CLI version
- `agent-go help` — show the main help text
- `agent-go help log` — show help for the log subcommand
- `agent-go log` — list the default log directory and recent log files
- `agent-go log <file>` — print a specific log file
- `agent-go --workspace <dir>` — run against a different workspace root
- `agent-go -p "<prompt>"` / `--prompt` / `-t` / `--task` — run a prompt in non-interactive mode

## Interactive commands

When the interactive shell is wired up, the runtime accepts:

- `/help`
- `/clear`
- `/history`
- `/stats`
- `/log`
- `/log <file>`
- `/exit`

Also accepted: `exit`, `quit`, and `q`.

## Runtime notes

- The CLI resolves relative workspace paths against the configured workspace directory.
- Log files live under `~/.agent-go/log/` by default.
- In the current bootstrap build, `help` and `log` are the stable commands; prompt and interactive execution print placeholder output until the runtime is fully wired.
