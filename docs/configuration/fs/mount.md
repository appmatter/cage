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
