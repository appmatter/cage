# Deny response (`network.plugins.egress.deny_response`)

Optional HTTP 403 body when egress denies a **plain HTTP** request after SOCKS CONNECT. Default **off** — without it, DENY closes the socket (curl: empty reply).

Agents can read the 403 and stop instead of treating the block as a flaky network error.

## Shape

```yaml
network:
  plugins:
    egress:
      deny_response:
        http: true # default false
        # message: optional override of the built-in text
      allow:
        - host: "*.example.com"
          method: GET
```

| Field | Meaning |
| --- | --- |
| `http` | When true, inject `HTTP/1.1 403 Forbidden` for plain-HTTP DENY |
| `message` | Optional body text; omit → built-in default (below) |

## Default message

*This is not a mistake: Cage sandbox egress intentionally blocked this request. Do not work around the block. Ask the user to consider permitting this destination (add a network.plugins.egress allow rule for the host, and method/path if required).*

The response also appends a detail line: `host:port METHOD path (reason)`.

## Limits

| Traffic | Behavior |
| --- | --- |
| Plain HTTP (peeked request-line) | 403 + message when `http: true` |
| TLS / non-HTTP | Still closes with no inject (no safe response to write) |

Hot-reloads with the egress/config chain (same as allow/deny rules). Profile leaf replaces the whole `deny_response` object when set.
