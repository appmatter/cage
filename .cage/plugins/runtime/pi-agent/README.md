# Pi agent config (host)

Edit `models.json` and `settings.json` here and commit. Synced into the **guest** `~/.pi/agent` on `cage vm start` / shell attach.
Does not write to your host Mac `~/.pi/agent`.

Default path: `.cage/plugins/runtime/pi-agent/`.

| File                        | Role                                                |
| --------------------------- | --------------------------------------------------- |
| `models.json`               | Provider endpoints                                  |
| `settings.json`             | Model, thinking, project trust                      |
| `APPEND_SYSTEM.md`          | Always-on Cursor-like coding loop                   |
| `AGENTS.md`                 | Short guest pointer                                 |
| `extensions/verify-done.ts` | Auto follow-up if edits stop without tests/evidence |
| `skills/implement-pr/`      | Optional long checklist (not required)              |

ChatGPT/Codex subscription (openai-oauth + http-proxy MITM): use provider **`openai-codex`**, not `openai` with a Codex `baseUrl`. Platform `openai-responses` sends rejected params (`max_output_tokens`, …) → opaque `400 (no body)`. Placeholder JWT `apiKey` is enough for account-id extract; host MITM injects real tokens. Details: [`plugins/secrets/openai-oauth`](../../../../plugins/secrets/openai-oauth/README.md#guest-pi).

Just ask the agent to implement something. No special prompt or `/skill` needed.
Re-attach shell or restart VM after edits so sync picks them up.
