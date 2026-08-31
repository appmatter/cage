# `fs.deny`

Deny list checked against mount/copy **host** paths. Matching entries fail load. Does not punch holes inside an allowed directory bind.

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

Profiles **union** deny entries onto the base list.

## Typical entries

| Entry                         | Why                                                                   |
| ----------------------------- | --------------------------------------------------------------------- |
| `.git`                        | Drop this and add `fs.mount` `.git` with `permission: ro` under a parent mount if the agent needs native git (read-only) |
| `.env`, `.ssh`, `credentials` | Real secrets stay on the host; seed via `fs.copy` if needed           |
| `.cage`, cage yaml          | Agent must not rewrite sandbox policy                                 |

## Rules

- Checked after merge, before start
- Globs allowed (e.g. `**/*.pem`)
- Removing a deny entry is required before mounting that path

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
