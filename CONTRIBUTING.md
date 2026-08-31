# Contributing

AI/LLM contributions are allowed (Obviously Cage is built for that). Commits must still be reviewed, understood and submitted by the human controlling the agent. Commit messages, code comments, test cases must be concise and worthwhile (Not AI slop no one wants to read). All code must follow the conventions in the existing codebase.

## Prerequisites

- Go 1.26+
- Node.js (for Prettier; `npm install` at repo root)
- [lefthook](https://github.com/evilmartians/lefthook) on PATH, then `lefthook install`
- [gitleaks](https://github.com/gitleaks/gitleaks) on PATH (secrets scan on commit)

```bash
go install github.com/evilmartians/lefthook@latest
brew install gitleaks
lefthook install
npm install
```

Pre-commit runs Prettier on staged `*.{md,yml,yaml,json}`, `gofmt` on staged `*.go`, and gitleaks on staged changes.

Dev loop (needs [Task](https://taskfile.dev)):

```bash
task              # delete VM, reinstall CLI+tart+egress, create, start
task recreate     # delete/create/start only
task start        # CONFIG=.cage/cage.docs-agent.yaml task start
task test:network # headless proxy+softnet smoke (skips if tart/softnet/privileges missing)
```

Or: `go test -tags network ./internal/network/` (darwin + network tag; same script).

Manual format:

```bash
npm run format       # check
npm run format:fix   # write
```

## Docs

- Generic pages and folders: **kebab-case** (`quick-starts/`, `project-structure.md`).
- Pages named for a config key or plugin id: **match the id** (`secrets_scanner.md`, `http-proxy.md`, `deny_response.md`).
