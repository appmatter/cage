# `fs.plugins.mention`

Installable **fs** plugin for host-side @mentions and search. Does **not** grant the agent filesystem access — that is `fs.mount` / `fs.copy` alone.

## Fields

| Field | Meaning |
| --- | --- |
| `include` | Globs to offer for mentions |
| `exclude` | Globs to skip |

If a profile sets `include` or `exclude`, that list **replaces** the base list for that key.

## Shape

```yaml
fs:
  plugins:
    mention:
      include:
        - "**/*"
      exclude:
        - "**/.git/**"
        - "**/.cage/**"
        - "**/.env"
```

## Related config

- [`fs`](../../../docs/configuration/fs/overview.md) — sandbox visibility
- [`fs.deny`](../../../docs/configuration/fs/deny.md) — mount/copy policy (separate from mention excludes)
