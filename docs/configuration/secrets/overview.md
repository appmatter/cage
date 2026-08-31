# Secrets (`secrets`)

Named stores nested by **plugin**, then alias. Resolved on the host. Config holds **references** only.

## Docs

| Doc                       | Topic                                        |
| ------------------------- | -------------------------------------------- |
| [backends](./backends.md) | Store plugins (`onepassword`, `keychain`, …) |

## Shape

```yaml
secrets:
  <plugin>:
    <alias>:
      vars:
        VAR_NAME: <ref>
```

Prefer `{{ secrets.<alias>.<var> }}` on `network.plugins.http-proxy` / other protocol proxies (alias unique across plugins). Same templates in `runtime.env` are allowed but put real values in the guest.

**No plugin `priority`.** Resolve order is a dependency DAG from `uses` and/or `{{ secrets.<alias>.* }}` in store config — map key order does not matter; cycles fail. Host SSO (`aws.sso_profile`) is reachability, not a secret-store dep. The map key under `secrets` is the install name — use `cage plugin install … --name` if two packages share a short name.

## Related config

- [`network.plugins` proxies](../network/plugins/overview.md) — preferred place for secret templates
- [`runtime.env`](../runtime/env/overview.md) — placeholders preferred; secret templates allowed but weaker
- [project structure](../../project-structure.md) — secrets context + DAG

## Example

```yaml
secrets:
  onepassword:
    personal-op:
      vars:
        OPENAI_API_KEY: op://Engineering/openai/api-key
```
