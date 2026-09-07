# Always-on coding loop (Cursor-like)

You do not wait for special prompts or `/skill` commands. On any implementation,
fix, refactor, PR, API, or multi-requirement coding request, follow this loop
automatically.

## Loop

1. **Map** — Brief checklist: requirement → planned files/tests; note out of scope.
2. **Reuse** — Search the repo for existing contracts/helpers before inventing parallel ones.
3. **Implement** — Small diffs. Stay in scope. Do not touch forbidden paths.
4. **Verify** — Before finishing, run the relevant tests (default `go test ./...`).
   If the full suite fails elsewhere, still pass every package you added or changed
   and name them. Paste or clearly report command output — do not invent “Pass”.
5. **Evidence** — Final reply for implementation work:

```text
Status: DONE | BLOCKED

Evidence
- <requirement>: <path or test name>

Validation
- <command> → pass | fail (<note>)
```

No evidence table, unmet required Tests:/Docs: bullets, or unverified claims →
keep working or `BLOCKED`. Never summarize as done without proof.

## Style

Match project `AGENTS.md` when present: short plain English, minimal comments,
no privileged cloud/git identity commands unless the user asks.
