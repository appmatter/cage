# Secrets (`secrets`)

Named stores under `secrets.plugins.<seat>`. Resolved on the host. Config holds **references** only.

## Docs

| Doc                       | Topic                                        |
| ------------------------- | -------------------------------------------- |
| [backends](./backends.md) | Store plugins (`onepassword`, `keychain`, …) |

## Shape

```yaml
secrets:
  plugins:
    <seat>:
      plugin: onepassword # omit when seat name == install name
      vars:
        VAR_NAME: <ref>
```

Prefer `{{ secrets.<seat>.<var> }}` on `network.plugins.http-proxy` / other protocol proxies. Same templates in `runtime.env` are allowed but put real values in the guest.

**No plugin `priority`.** Resolve order is a dependency DAG from `uses` and/or `{{ secrets.<seat>.* }}` in store config — map key order does not matter; cycles fail. Host SSO (`aws.sso_profile`) is reachability, not a secret-store dep. Seat key is the config alias; optional `plugin:` / `package:` match other contexts ([project structure](../../project-structure.md)).

## Related config

- [`network.plugins` proxies](../network/plugins/overview.md) — preferred place for secret templates
- [`runtime.env`](../runtime/env/overview.md) — placeholders preferred; secret templates allowed but weaker
- [project structure](../../project-structure.md) — secrets context + DAG

## Example

See [`examples/onepassword`](../../../examples/onepassword/) and [backends](./backends.md). Project `.cage/cage.yaml` keeps `secrets.plugins: {}` until you add real seats.