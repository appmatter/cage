# `runtime.env`

Injected into the sandbox process environment. **Only keys listed under `runtime.env` are sent to the guest** — the host process environment is never copied (secrets/keys on the host stay on the host unless you explicitly map them here).

**Prefer** placeholders here and `{{ secrets.* }}` on protocol proxies under `network.plugins` so real values stay on the host. Templating secrets into `runtime.env` is allowed when a tool cannot go through a proxy — that puts the value in the guest.

## Shape

```yaml
runtime:
  env:
    APP_MODE: dev
    OPENAI_API_KEY: "fake-key-for-agent"
    # only when a guest tool cannot use http-proxy inject:
    # DB_PASSWORD: "{{ secrets.onepassword.DB_PASSWORD }}"
```

Cage resolves `{{ secrets.<seat>.<var> }}` on the host at `vm start` (same pass as http-proxy), then installs the substituted map into the guest.

Profiles deep-merge; leaf wins on key conflict.

## Related config

- [`secrets`](../../secrets/backends.md) — host refs; resolve into proxies (preferred) or env when needed
- [`network.plugins` proxies](../../network/plugins/type.md) — where secret templates usually belong
