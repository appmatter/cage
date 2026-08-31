# Runtime backend plugins

Contract for **backend** stage plugins (`tart`, `incus`, `hyperv`, …). Same rules on every host OS. Go surface: `pkg/plugin/v1/runtime`. Conformance checklist: [runtime-backend-conformance](./runtime-backend-conformance.md).

Harness plugins (`pi-agent`, …) are a different stage — see [project structure](../project-structure.md). Heavy guest installs use [derived image bake](./runtime-image-bake.md); `Create` may target a cached derived image instead of the seat’s base `image`.

## Interface

Implement `runtime.Backend`:

| Method | Role |
| --- | --- |
| `Name` | Short plugin id (e.g. `tart`) |
| `Create` | Provision from `Spec.Image` (base or derived bake id) |
| `Start` | Bring guest up; apply mounts/copies; run seat lifecycle scripts |
| `Stop` / `Status` / `Delete` | Lifecycle |
| `Exec` | Run argv in the guest (stdin optional). Required for harness, proxy env inject, seat scripts |
| `Bake` | Materialize derived image from base + scripts if missing; no-op on cache hit. See [runtime-image-bake](./runtime-image-bake.md) |

Manifest: `context: runtime`, `stage: backend`. No `priority` in the manifest — operators set that in config.

## `Spec` fields the plugin must honor

| Field | Plugin duty |
| --- | --- |
| `ID`, `Image`, `Workdir` | Identity and guest cwd for scripts |
| `Graphics` | UI vs headless when the backend supports it |
| `Mounts` / `Copies` | Expose or copy into the guest on `Start` |
| `OnCreate` / `OnStart` / `OnDestroy` | Host script paths; plugin chooses interpreter (`sh`, `powershell`, …) |
| `ExtraRunArgs` | **Append unchanged** to the backend’s run/launch command. Empty = none |

Do not invent Cage network policy inside the plugin. Core owns allowlists and the host proxy.

## Network split (all OSes)

When `network.proxy.disabled` is false/omitted:

1. **Core** starts the host HTTP CONNECT (+ SOCKS) proxy with MITM by default, loads filter plugins (e.g. egress), optionally logs CONNECTs (`network.proxy.logging`).
2. **Core** fills `Spec.ExtraRunArgs` with whatever locks guest egress to the **host only** on this platform (today: softnet flags on darwin/Tart). Other backends map the same idea to their mechanism (nftables, Hyper-V switch ACLs, …) — still via `ExtraRunArgs` or an equivalent pass-through the contract already provides.
3. **Core** uses `Exec` after start to install guest proxy env + MITM CA (`/var/lib/cage/proxy.env`, `profile.d`, `/etc/environment`, ca-certificates) so processes inherit `http://` proxy settings without manual `source`.
4. **Backend** only applies `ExtraRunArgs` and provides a working `Exec`. It does **not** parse egress YAML or softnet CIDR lists.

When proxy is disabled, `ExtraRunArgs` is empty and core skips proxy start / env inject. Guest networking is the backend default (open).

```
guest → (host-only lock) → host HTTP CONNECT (+ MITM) → filter → dial upstream
              ↑ ExtraRunArgs                         ↑ core
```

## Start order (plugin-owned body)

Typical `Start` sequence (Tart follows this; others should match):

1. Ensure VM running (apply `ExtraRunArgs` on first run/launch).
2. Mounts → copies.
3. Seat `on-create` once (marker in guest), then `on-start` every time.
4. Return. Core may then inject proxy env and start a harness.

If the backend restarts the guest for graphics/UI, re-apply mounts and keep `ExtraRunArgs` on the relaunch.

## Don’t

- Hard-code softnet, CIDRs, or egress rules in the runtime plugin.
- Drop or rewrite `ExtraRunArgs`.
- Skip `Exec` — core and harnesses depend on it.
- Declare `priority` in the plugin manifest.
