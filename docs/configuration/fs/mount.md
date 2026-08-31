# `fs.mount`

Live binds from host into the sandbox. Writes with `permission: rw` go back to the host.

Maps are keyed by **guest target**. Under `fs.layout.mode: flat`, targets sit under `runtime.workdir`.

## Shape

```yaml
fs:
  mount:
    src: ./src # shorthand: target → host
    tests:
      host: ./tests
      permission: ro # rw | ro (default rw)
    scripts:
      host: ./scripts
      permission: rw
```

In a profile, drop a base mount with:

```yaml
fs:
  mount:
    tests:
      remove: true
```

## Rules

- Directory binds only (no single-file binds as the primary model)
- Host path must not match `fs.deny`, or load fails
- Merged by target key across `cage.yaml` and `cage.<name>.yaml`
- Nested guest path under another mount (e.g. `.git` or `scratch` under `"."`):
  - `permission: ro` — own share + bind, then `remount,bind,ro` (host honored)
  - `permission: rw` — writable hole under an `ro` parent (own share + bind; path must exist)

## Related config

- [`fs.copy`](./copy.md) — seed files without a live bind
- [`fs.deny`](./deny.md) — deny list
- [`fs.layout`](./layout.md) — how targets are rooted

## Example

```yaml
fs:
  mount:
    src: ./src
    tests:
      host: ./tests
      permission: ro
```

Repo root rw + `.git` read-only (history without commit/push):

```yaml
fs:
  mount:
    ".": .
    .git:
      host: .git
      permission: ro
```

Repo root read-only + writable nest:

```yaml
fs:
  mount:
    ".":
      host: .
      permission: ro
    scratch:
      host: ./scratch
      permission: rw
```
