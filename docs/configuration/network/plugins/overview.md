# Proxies (`network.plugins.*-proxy`)

**Terminate** stage of `network.traffic`. Each protocol is its own installable plugin under `network.plugins` (no `proxies:` bag). Host-side endpoints the agent calls instead of talking to upstreams with real credentials.

Upstream hosts must also pass `network.plugins.egress` (filter is not skipped).

If **two or more** terminate plugins are present, each must set `priority` explicitly (unique; lower runs first). Alone → omit (default `1`). Priority is operator config only.

## Docs

| Doc | Topic |
| --- | --- |
| [type](./type.md) | Protocol plugins (`http-proxy`, `postgres-proxy`, …) |
| [http-proxy](../../../../plugins/network/http-proxy/README.md) | Host reverse-HTTP + header inject (v1) |
| [egress](../../../../plugins/network/egress/README.md) | Outbound allowlist (filter) |

## Shape

```yaml
network:
  plugins:
    http-proxy:
      priority: 1
      package: git:github.com/acme/cage-http-proxy  # optional
      <name>:
        url: https://api.example.com/v1
        headers:
          Authorization: "Bearer {{ secrets.<seat>.<var> }}"
    postgres-proxy:
      priority: 2
      <name>:
        listen: 5432
        host: db.example.com
```

`priority` and `package` are reserved on the plugin seat (not endpoint names). Omit a plugin key entirely if unused.

## Related config

- [`secrets`](../../secrets/backends.md) — refs used in templates
- [`runtime.env`](../../runtime/env/overview.md) — placeholders in the sandbox only
- [`network.plugins.egress`](../../../../plugins/network/egress/README.md) — outbound allowlist
- [`fs.plugins.secrets_scanner`](../../../../plugins/fs/secrets_scanner/README.md) — context hook actions
