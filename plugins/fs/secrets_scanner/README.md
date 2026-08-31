# Hooks

Hook points live **on the context** (or on a plugin under `*.plugins`) — not a top-level `hooks:` map.

Installing a plugin under `<context>.plugins` is enough for that plugin’s default lifecycle. Use `<context>.hooks` only for extra wiring.

```yaml
fs:
  plugins:
    secrets_scanner:
      on_find: warn
      allow:
        - OPENAI_API_KEY
        - path: .env.example
```

| Location | Meaning |
| --- | --- |
| `<context>.plugins.<name>` | Plugin config seat; plugin registers its default hooks |
| `<context>.hooks.<event>` | Extra runs — list plugin names when defaults aren’t enough |

## `fs.plugins.secrets_scanner`

Installable **fs** plugin. Presence enables scanning of guest-visible surfaces. No separate hooks list required.

```yaml
fs:
  plugins:
    secrets_scanner:
      on_find: warn # warn | fail
      allow:
        - OPENAI_API_KEY
        - path: .env.example
        - pattern: "fake-key-*"
```

`allow` suppresses matching findings so known placeholders don’t trip warn/fail.

## Related config

- [project structure](../../../docs/project-structure.md) — contexts, plugins, hook points
- [`fs`](../../../docs/configuration/fs/overview.md) — `plugins.secrets_scanner` seat
- [`network.plugins.egress`](../../network/egress/README.md) — outbound allowlist
