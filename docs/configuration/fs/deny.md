# `fs.deny`

Deny list for mount/copy **host** paths and for **descendants under allowed binds**.

1. Matching mount/copy hosts fail load.
2. Matching paths that exist under an allowed mount host are masked in the guest at VM start (directory → mode-0 tmpfs; file → empty ro bind). Explicit mount guest roots are not masked.

Live virtiofs: paths created on the host after start can still appear until the next start.

## Shape

```yaml
fs:
  deny:
    - .git
    - .env
    - .ssh
    - credentials
    - .cage
    - .cage/cage.yaml
    - .cage/cage.*.yaml
    - "**/*.pem"
    - "**/*.key"
```

Profiles **union** deny entries onto the base list. Turn a base entry off with `active: false` (exact path match):

```yaml
# cage.dogfood.yaml
extends: cage.yaml
fs:
  mount:
    .cage:
      host: .cage
      permission: ro
  deny:
    - path: .cage
      active: false
    - path: .cage/cage.yaml
      active: false
    - path: .cage/cage.*.yaml
      active: false
```

## Typical entries

| Entry                         | Why                                                                   |
| ----------------------------- | --------------------------------------------------------------------- |
| `.git`                        | Drop this and add `fs.mount` `.git` with `permission: ro` under a parent mount if the agent needs native git (read-only) |
| `.env`, `.ssh`, `credentials` | Real secrets stay on the host; seed via `fs.copy` if needed           |
| `.cage`, cage yaml          | Agent must not rewrite sandbox policy                                 |

## Rules

- Checked after merge, before start
- Globs allowed (e.g. `**/*.pem`)
- Removing a deny entry is required before mounting that path as its own target (`path` + `active: false` in a profile)
- Under a parent mount (e.g. `".": .`), matching descendants are masked at start — not rejected at load

## Related config

- [`fs.mount`](./mount.md) / [`fs.copy`](./copy.md) — what deny constrains
- [`mention`](../../../plugins/fs/mention/README.md) — host @mention excludes (separate from deny)

## Example

```yaml
fs:
  mount:
    src: ./src
  deny:
    - .git
    - .env
    - .cage
```
