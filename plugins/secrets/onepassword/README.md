# 1Password (`secrets.plugins`)

Host-side secrets store: resolves `op://…` refs with the [1Password CLI](https://developer.1password.com/docs/cli) (`op read`). Values stay on the host for protocol-proxy inject.

## Install

```bash
brew install 1password-cli   # provides `op`
# Required for Touch ID: Settings → Developer → Integrate with 1Password CLI
# (+ Settings → Security → Touch ID)
cage plugin install -l ./plugins/secrets/onepassword
```

## Shape

```yaml
secrets:
  plugins:
    onepassword: # seat == install name → omit plugin:
      account: my.1password.com # optional → op --account
      app: true # omit = true; desktop CLI integration (Touch ID via app)
      vars:
        OPENAI_API_KEY: op://Vault/item/field

network:
  plugins:
    http-proxy:
      openai:
        url: https://api.openai.com/v1
        headers:
          Authorization: "Bearer {{ secrets.onepassword.OPENAI_API_KEY }}"
```

Multi-account: second seat with `plugin: onepassword` + `account:` — see [backends](../../../docs/configuration/secrets/backends.md).

## Notes

- Cage topo-resolves seats during foreground `cage vm start`, then calls this plugin (`Configure` account, `Resolve` refs).
- Resolve uses `op read` with seat `account` / `app` from cage.yaml (`app` omit/true → desktop CLI integration / Touch ID).
- Detached `proxy-serve` never talks to `op`; it only receives already-substituted config. `.cage/run/*/http-proxy.yaml` keeps templates.
