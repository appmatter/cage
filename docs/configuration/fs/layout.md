# `fs.layout`

Controls how `fs.mount` / `fs.copy` targets are placed in the guest.

## Modes

| `mode` | Behavior                                                              |
| ------ | --------------------------------------------------------------------- |
| `flat` | Targets relative to `runtime.workdir` (e.g. `src` → `/workspace/src`) |
| `host` | Preserve host path structure for path-sensitive tools                 |

Default in examples and init: `flat`.

## Shape

```yaml
fs:
  layout:
    mode: flat
  mount:
    src: ./src # → /workspace/src when workdir is /workspace
```

## Related config

- [`runtime.workdir`](../runtime/overview.md) — root for `flat`
- [`fs.mount`](./mount.md) / [`fs.copy`](./copy.md) — targets interpreted with this mode
