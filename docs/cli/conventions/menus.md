# Interactive menus

Standard for any Cage command that asks the operator to pick one or more items.

Shared helper: `internal/climenu` (huh).

## Dual surface (required)

Every menu must expose **equal** capability both ways:

| Surface | When | Example |
| --- | --- | --- |
| **Interactive** | TTY, no selector args | `cage plugin init` |
| **Flags / args** | Scripts, CI, non-TTY, or skip the UI | `cage plugin init runtime/pi-agent` |

Same outcomes, same validation. Non-TTY with no explicit selector → clear error (`pass kind/name`), never hang on stdin.

## Interactive UX (required)

Use **[charmbracelet/huh](https://github.com/charmbracelet/huh)** via `climenu` — not ad-hoc `fmt` + `scanf`.

| Style | How | Use |
| --- | --- | --- |
| **Checkbox multi-select** | ↑↓ · **space** toggle · **ctrl+a** all/none · enter | `climenu.Multi` (default TUI) |
| **Number select** | type `1`, `2`, … · enter (`0` finishes multi) | `CAGE_ACCESSIBLE=1` (or `ACCESSIBLE=1`) — huh accessible mode |

Options are always numbered in the TUI (`1) …`) so indices match accessible mode. `/` filters in the TUI.

- Single-choice → `climenu.One`
- Multi-choice → `climenu.Multi`
- Still show the menu on a TTY when there is only one candidate (operator confirms)

## Do / don’t

- **Do** list only relevant candidates (e.g. plugins that advertise `init`).
- **Do** print what ran after confirm (`initialized runtime/pi-agent → …`).
- **Don’t** auto-skip the UI when there is exactly one candidate — still show the menu on a TTY.
- **Don’t** add another TUI library — stick to huh / `climenu`.

## Related

- [CLI conventions](./index.md) — command shape
- [macOS quick start](../quick-starts/macos/index.md)
- [Derived image bake](../../plugins/runtime-image-bake.md) — `bake delete` select
