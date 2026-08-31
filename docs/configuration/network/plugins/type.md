# Proxy plugins

Each key under `network.plugins` ending in `-proxy` (or protocol-specific seats like `http-proxy`) is an installable plugin. Cage terminates that protocol on a local endpoint, applies secrets on the host, and forwards.

How Cage reaches a private upstream (VPN, SSO, bastion) is configured on the named proxy entry.

## Plugins

| Plugin | Protocol | Use for |
| --- | --- | --- |
| `http-proxy` | HTTP/HTTPS | REST APIs and most webhooks. Auth via header templates. |
| `aws-proxy` | HTTP/HTTPS | AWS APIs (e.g. boto3). Host re-signs; sandbox gets placeholders via `env`. |
| `postgres-proxy` | Postgres (TCP) | Database clients. Agent connects to the proxy `listen` port. |
| `tcp-proxy` | Raw TCP | Generic tunnels when Cage does not need to understand the protocol. |
| `websocket-proxy` | WebSocket | Long-lived socket APIs. |
| `grpc-proxy` | gRPC | gRPC services. |

JSON-RPC over HTTP uses `http-proxy`.

## Rollout

- **v1:** `http-proxy` — see [http-proxy](../../../../plugins/network/http-proxy/README.md)
- **Later:** `postgres-proxy`, `tcp-proxy`, `aws-proxy`, `websocket-proxy`, `grpc-proxy`

## Related config

- **`secrets`** — prefer templates in proxy fields (`{{ secrets.<seat>.<var> }}`)
- **`runtime.env`** — placeholders preferred; `{{ secrets.* }}` allowed but puts values in the guest
- **Reachability** — optional blocks on the proxy (e.g. `aws.sso_profile`) for private upstreams

## Examples

```yaml
network:
  plugins:
    http-proxy:
      priority: 1
      openai:
        url: https://api.openai.com/v1
        headers:
          Authorization: "Bearer {{ secrets.personal-op.OPENAI_API_KEY }}"
    postgres-proxy:
      priority: 2
      development-postgres:
        listen: 5432
        host: development-postgres.example.com
        port: 5432
        database: development-postgres
        username: "{{ secrets.dev-sm.DB_USERNAME }}"
        password: "{{ secrets.dev-sm.DB_PASSWORD }}"
        ssl: require
        aws:
          sso_profile: project-dev-developer
          region: eu-west-2
```

## Connecting to AWS

**AWS APIs (boto3, AWS CLI)** — use plugin `aws-proxy`. Sandbox gets placeholder keys and an endpoint pointing at Cage.

**Private data plane (RDS, etc.)** — use `postgres-proxy` / `tcp-proxy` and an `aws` reachability block on that entry.

```yaml
runtime:
  env:
    AWS_ACCESS_KEY_ID: "fake-key-for-agent"
    AWS_SECRET_ACCESS_KEY: "fake-key-for-agent"
    AWS_ENDPOINT_URL: "http://cage-aws:3456"

network:
  plugins:
    aws-proxy:
      default:
        aws:
          sso_profile: project-dev-developer
          region: eu-west-2
```
