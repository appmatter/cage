# Color

Host CLI status lines use **`internal/termlog`** (`github.com/fatih/color`). Don’t invent a second palette or call color APIs from command handlers.

## Roles

| Helper                | Color                           | Meaning                                                             |
| --------------------- | ------------------------------- | ------------------------------------------------------------------- |
| `termlog.CLI`         | cyan bold                       | Cage host status (`cage: …`) → stderr                             |
| `termlog.Plugin`      | bright magenta                  | Runtime plugin status (`tart: …`) → stderr                          |
| `termlog.GuestWriter` | bright green prefix `guest \| ` | Guest/script stdout lines → stderr; optional raw copy to a log file |

Machine-readable output (JSONL proxy logs, inspect tables meant for pipes) stays **uncolored**.

## Rules

- Status chatter → `termlog`, not `fmt` + ad-hoc color.
- Respect `NO_COLOR` / non-TTY (fatih/color already does).
- huh menus keep **huh’s default theme** — don’t restyle per command.
- Don’t reuse guest green or plugin magenta for unrelated CLI messages.

## Related

- [CLI conventions](./index.md)
- [Interactive menus](./menus.md)
