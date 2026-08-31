# CLI conventions

Standards for Cage CLI commands and prompts.

## Command shape

```bash
cage <noun> <verb>              # interactive when a choice is needed
cage <noun> <verb> <id…>        # explicit; no prompt
cage <noun> <verb> --all        # bulk when that action exists
```

- Singular nouns: `plugin`, `bake`, `vm`, `config` (not `plugins.foo`).
- Identity: `kind/name` (e.g. `runtime/pi-agent`) or opaque ids (bake hash).
- Prefer one short help line under a prompt title over a separate man page.

## Topics

- [Interactive menus](./menus.md) huh / `climenu`, dual surface, checkbox + numbers
- [Color](./color.md) — termlog roles (CLI / plugin / guest)

## Related

- [macOS quick start](../quick-starts/macos/index.md)
- [Derived image bake](../../plugins/runtime-image-bake.md)
