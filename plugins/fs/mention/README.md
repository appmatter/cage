# `fs.plugins.mention`

Installable **fs** plugin for host-side @mentions and search. Index policy is independent of `fs.mount` and `fs.copy`.

A selected guest-visible file is referenced by its guest path. A selected host-only file is sent as a bounded snapshot. A host-only directory is sent as a bounded recursive listing. Absolute host paths never leave Cage.

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

## Path mapping

Cage supplies resolved mount and copy mappings to the plugin. `flat` maps targets below `runtime.workdir`; `host` maps them below `/`. A result includes its project-relative path, `file` or `directory` kind, and guest path when visible to the VM.

See [requirements](docs/requirements.md).

## Related config

- [`fs`](../../../docs/configuration/fs/overview.md) — sandbox visibility
- [`fs.deny`](../../../docs/configuration/fs/deny.md) — mount/copy policy (separate from mention excludes)
