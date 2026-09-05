# Pi agent config (host)

Synced into the **guest** `~/.pi/agent` on `cage vm start` / shell attach.
Does not write to your host Mac `~/.pi/agent`.

| File                        | Role                                                |
| --------------------------- | --------------------------------------------------- |
| `models.json`               | Provider endpoints                                  |
| `settings.json`             | Model, thinking, project trust                      |
| `APPEND_SYSTEM.md`          | Always-on Cursor-like coding loop                   |
| `AGENTS.md`                 | Short guest pointer                                 |
| `extensions/verify-done.ts` | Auto follow-up if edits stop without tests/evidence |
| `skills/implement-pr/`      | Optional long checklist (not required)              |

Just ask the agent to implement something. No special prompt or `/skill` needed.
Re-attach shell or restart VM after edits so sync picks them up.
