# `fs.copy`

One-shot copy from host into the guest at sandbox start. Use this to seed files (e.g. `.env.example` → `.env`) without mounting real secrets.

Maps are keyed by **guest target**. String value is the host source; map form sets `host` and optional `permission`.

## Shape

```yaml
fs:
  copy:
    .env: .env.example
    config.local.json:
      host: ./config.example.json
      permission: rw # rw | ro
```

In a profile, replace or remove by target key (`remove: true`).

## Rules

- Runs at start; later host edits are not synced (unlike `mount`)
- Host source must not match `fs.deny`, or load fails
- Prefer `copy` for secret _templates_; keep real `.env` / keys on `deny`

## Related config

- [`fs.mount`](./mount.md) — live binds
- [`fs.deny`](./deny.md) — deny list
- [`env`](../runtime/env/overview.md) — process env in the sandbox (separate from files)

## Example

```yaml
fs:
  copy:
    .env: .env.example
  deny:
    - .env
```
