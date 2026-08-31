# `runtime.env`

Injected into the sandbox process environment.

**Prefer** placeholders here and `{{ secrets.* }}` on protocol proxies under `network.plugins` so real values stay on the host. Templating secrets into `runtime.env` is allowed when a tool cannot go through a proxy — understand that puts the value in the guest.

## Shape

```yaml
runtime:
  env:
    APP_MODE: dev
    OPENAI_API_KEY: "fake-key-for-agent"
```

Profiles deep-merge; leaf wins on key conflict.

## Related config

- [`secrets`](../../secrets/backends.md) — host refs; resolve into proxies (preferred) or env when needed
- [`network.plugins` proxies](../../network/plugins/type.md) — where secret templates usually belong
