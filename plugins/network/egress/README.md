# Egress (`network.plugins.egress`)

Outbound allow/deny — **filter** stage of `network.traffic`. Runs on Cage’s **host SOCKS5 proxy** (default), not as softnet CIDR lists.

When `network.proxy.disabled` is false/omitted (default), guest traffic is forced through the host proxy (softnet host-only so the guest cannot bypass). Egress `Check` decides allow/deny for each CONNECT (host, optional port / method / path).

Proxied upstreams must appear here too — protocol proxies do **not** skip the filter.

Lone filter plugin: omit `priority` (defaults to `1`). See [project structure](../../../docs/project-structure.md).

## Enforcement

| Layer | What it checks |
| --- | --- |
| Softnet (when proxy on) | Host-only (`@host`) — blocks direct internet bypass |
| Host SOCKS5 + egress `Check` | host, port; method/path when known (see below) |

`network.proxy.disabled: true` turns off the SOCKS proxy and softnet lock (open guest networking; debug escape).

`network.proxy.logging: true` writes CONNECT events as JSONL to `.cage/run/<id>/proxy.log` (not the start TTY — use follow below). Softnet drops outside SOCKS are not per-packet; with logging on you also get advisory `SOFTNET` lines (active + start probe) — see [softnet](../../runtime/tart/docs/softnet.md). Follow with:

```bash
cage vm logs -f
# or start and follow in one shot:
cage vm start -f
# or: tail -f .cage/run/cage-vm/proxy.log
```

Requires [softnet](https://github.com/cirruslabs/softnet) on PATH **with privileges** when proxy is on (vmnet/DHCP setup). Install + setuid:

```bash
brew trust --formula cirruslabs/cli/softnet
brew install cirruslabs/cli/softnet
sudo chown root "$(brew --prefix softnet)/bin/softnet"
sudo chmod u+s "$(brew --prefix softnet)/bin/softnet"
```

See [Softnet (macOS)](../../runtime/tart/docs/softnet.md).

Install the plugin: `cage plugin install -l ./plugins/network/egress`.

On start, Cage installs guest proxy env so agents/shells need not source anything by hand:

- `/var/lib/cage/proxy.env` — exports (`HTTP_PROXY=http://<gateway>:<http_port>`, …) for the host HTTP CONNECT proxy (MITM by default)
- `/etc/profile.d/cage-proxy.sh` — sources it for login shells
- `/etc/environment` — resolved `KEY=value` for PAM / many tools
- MITM CA in guest trust store + `NODE_EXTRA_CA_CERTS=/var/lib/cage/ca.pem`

## Method / path

HTTP CONNECT soft-checks host:port first. With MITM (default), TLS is broken and method/path are filled for egress. With `network.proxy.mitm: false`, CONNECT is tunneled and HTTPS method/path stay empty (same as old SOCKS peek).

| Client traffic | Method/path |
| --- | --- |
| Plain HTTP | Filled |
| HTTPS via MITM | Filled |
| HTTPS tunnel (`mitm: false`) | Empty; rules that require `method` / `path` **deny** |

Prefer real `https://…` URLs through MITM. Named [`http-proxy`](../http-proxy/README.md) listen ports remain for clear-HTTP legacy.

## Hot reload

Proxy watches the active cage config chain (`cage.yaml` + extends) and `.cage/run/<id>/egress.yaml`. Edit either — no proxy/VM restart. Reloads show as `proxy RELOAD egress (config|egress.yaml)` in `cage vm logs -f` / proxy.log. Invalid YAML logs `proxy RELOAD egress (error: …)` and keeps previous rules.

## Shape

```yaml
network:
  proxy:
    disabled: false # omit = proxy ON
    logging: true   # optional: CONNECT JSONL → .cage/run/<id>/proxy.log
    mitm: true      # omit = HTTPS MITM on; false = tunnel only
  plugins:
    egress:
      deny_response:
        http: false # see docs/deny-response.md
      deny:
        - host: evil.example
          port: 443
      allow:
        - host: "*.package-readme.com" # www + apex
        - host: registry.npmjs.org
          port: 443
        - host: api.openai.com
          port: 443
          method: POST
          path: /v1/chat/completions
        - host: db.example.com
          port: 5432
```

| Field | Meaning |
| --- | --- |
| `priority` | Optional; required + unique when ≥2 plugins share filter |
| `package` | Optional install source when short name `egress` collides |
| `deny_response` | Optional HTTP 403 on plain-HTTP DENY — [deny_response](./docs/deny-response.md) |
| `deny` | Matched first → always deny |
| `allow` | Destinations the sandbox may reach via the host proxy |
| `host` | Hostname; glob `*` / `*.example.com` (also matches apex `example.com`); `"*"` = any |
| `port` | Optional TCP port; omit = any |
| `method` | Optional HTTP method; omit = any (see Method / path) |
| `path` | Optional URL path glob; omit = any |

Evaluation: deny match → deny; else allow match → allow; else if `allow` is non-empty → deny; else allow (deny-only mode).

Profiles: if a profile sets `allow`, `deny`, or `deny_response`, that value **replaces** the base.
