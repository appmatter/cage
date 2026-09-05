# HTTP proxy (`network.plugins.http-proxy`)

**Terminate** stage of [`network.plugins`](../../../docs/configuration/network/plugins/overview.md): header inject for named API hosts.

With **HTTPS MITM** on (default when host proxy is on), the agent calls the real URL (`https://api.openai.com/...`). Cage matches `url` host, injects `headers`, runs [egress](../egress/README.md) `Check` with Method/Path, then dials upstream. No `CAGE_HTTP_*` rewrite required.

Named `listen` ports still work as clear-HTTP reverse proxies (legacy / tools that cannot trust the guest CA). Each VM’s proxy only accepts that guest’s source IP (peers cannot use each other’s inject endpoints).

## Shape

```yaml
network:
  proxy:
    mitm: true # omit = on when proxy enabled; false = CONNECT tunnel only
  plugins:
    http-proxy:
      priority: 1 # required when ≥2 terminate plugins
      openai:
        url: https://api.openai.com/v1
        headers:
          Authorization: "Bearer {{ env.OPENAI_API_KEY }}"
        # listen: 18080  # optional legacy clear-HTTP endpoint
```

| Field | Meaning |
| --- | --- |
| `url` | Upstream base (host used for MITM Host match; path used by legacy listen join) |
| `headers` | Injected on upstream (override guest) |
| `listen` | Optional legacy clear-HTTP bind; `0`/omit = ephemeral. Prefer omit — fixed ports clash when multiple cages (or worktrees) run on one host. MITM does not need `listen`. |

Install: `cage plugin install -l ./plugins/network/http-proxy`.

## Guest usage (MITM)

```bash
curl -sS -X POST "https://api.openai.com/v1/chat/completions" \
  -H 'content-type: application/json' \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'
```

Guest trusts `.cage/.cache/ca` via `/usr/local/share/ca-certificates/cage-mitm.crt` + `NODE_EXTRA_CA_CERTS`. Pinning-broken clients will fail.

## Legacy clear-HTTP

On VM start, Cage still writes `/var/lib/cage/http-proxy.env` when listen ports exist:

```bash
export CAGE_HTTP_OPENAI_URL=http://$GW:18080
```

## Templates (v1)

| Form | Behavior |
| --- | --- |
| literal | Passed through |
| `{{ env.NAME }}` | Host environment at Configure time |
| `{{ secrets.<seat>.<var> }}` | Resolved by Cage before Configure (install secrets plugin; values not written to http-proxy.yaml) |

Set `env.*` keys on the **host** before `cage vm start`, or configure `secrets.plugins` + `cage plugin install -l ./plugins/secrets/onepassword`.

## State / reload

- CA: `.cage/.cache/ca/{ca.pem,ca.key}` (working tree; key never enters guest)
- Proxy ports: `.cage/run/<id>/proxy.json` (`http_port`, SOCKS `port`)
- Legacy listen: `.cage/run/<id>/http-proxy.json`
- Secret templates (`{{ secrets.* }}`) are re-resolved by proxy-serve on each
  prepare after `secrets.refresh_interval` (default `2m`) and http-proxy is
  re-Configured (OAuth / `op read` refresh); **listen ports stay put** until
  proxy restart
- Egress allowlist hot-reloads from cage config independently
