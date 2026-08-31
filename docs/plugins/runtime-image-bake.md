# Derived image bake

Heavy guest installs belong in a **content-addressed derived image**, not per-VM `on-create`.

## Hook: `runtime.before_bake`

Plugins declare hook attachments at **build time** (`plugin.json` → manifest `hooks`). Seat under `runtime.plugins` → core invokes them.

| Location | Meaning |
| --- | --- |
| `runtime.plugins.<name>` | Seat present → plugin’s declared hooks (e.g. `before_bake`) |
| `runtime.hooks.<event>` | Extra plugin names for that event |
| Seat YAML `bake:` | Optional operator script paths (same hash bucket) |

`cage config inspect` prints **hooks:** with the fully resolved event → plugin list (YAML + plugin-declared). Soft **egress_hints** from `plugin.json` are listed and warned when missing from `network.plugins.egress.allow` (also on `vm start`).

## Flow

1. Resolve hooks (`before_bake` plugins).
2. Each plugin’s `BeforeBake` returns script attachments; core writes them under `.cage/.cache/images/attachments/`.
3. Hash attachments + operator `bake:` scripts → `cage-bake-<16 hex>`.
4. Backend `Bake` on miss; Create from derived image.

## Config

```yaml
runtime:
  plugins:
    tart:
      image: ghcr.io/cirruslabs/ubuntu:latest
    pi-agent:
      version: "0.84.4"
      # agent_dir: .cage/plugins/runtime/custom   # optional override
      packages:
        - npm:@acme/pi-tools@1.2.3
```

Default host agent dir (when `agent_dir` omitted): `.cage/plugins/runtime/<plugin>/`  
Seed with [`cage plugin init`](../cli/conventions/menus.md) (interactive or `runtime/pi-agent`). Plugin also seeds on start if missing; synced to guest `~/.pi/agent`. Commit that directory (binaries live under `.cage/.cache/plugins/`).

## CLI

```bash
cage bake list
cage bake delete                         # interactive multi-select
cage bake delete d6c7e0d5d9c33b39        # one or more ids
cage bake delete --all                   # every bake image + .cage/.cache/images stamps
```

Deletes the backend image (Tart VM) and host `.txt` / `.ok` stamps. Does not delete sandbox VMs (`cage-vm`) — use `cage vm delete`.

## Related

- [CLI menus](../cli/conventions/menus.md) — interactive + flags (`climenu` / huh)
- [project structure](../project-structure.md) — hook points
- [runtime config](../configuration/runtime/overview.md)
- [runtime backend](./runtime-backend.md)
