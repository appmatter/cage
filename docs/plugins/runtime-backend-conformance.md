# Runtime backend conformance

Behaviors every **backend** runtime must satisfy (and that we should re-prove when adding Incus / Hyper-V / Docker). Contract: [runtime-backend](./runtime-backend.md).

Core owns proxy + egress policy. The backend owns applying `ExtraRunArgs`, mounts/copies, lifecycle, and `Exec`. Tests below are **outcome**-based so each OS can use its own lock mechanism.

Legend: **Must** = fail CI if broken on that backend. **Should** = required before calling the backend production-ready. Scripts today are darwin/Tart-oriented; other backends need equivalent harnesses.

## Lifecycle & FS

| # | Requirement | How to prove | Tart today |
| --- | --- | --- | --- |
| L1 | Create → start → status `running` → stop → delete | smoke / IT | `task test:integration`, `task test:network` |
| L2 | `Exec` runs argv in guest; exit status reflected | one-shot `cage vm exec -- true` / `false` | network scripts, ITs |
| L3 | Interactive `Exec` with TTY (login shell) | `cage vm exec` (no args) | manual / CLI |
| L4 | Mounts appear at guest paths after start | write host file, read in guest | `TestIntegrationMountAndCopy` |
| L5 | Copies land in guest after start | same | `TestIntegrationMountAndCopy` |
| L6 | Seat `on-create` once; `on-start` every start | marker + script side effect | tart lifecycle |
| L7 | `ExtraRunArgs` appended unchanged on run/launch | inspect process args / equivalent | softnet ITs |

## Network — proxy on (`network.proxy.disabled` omit/false)

Host-only lock + SOCKS + egress. Backend must not weaken this.

| # | Requirement | How to prove | Tart today |
| --- | --- | --- | --- |
| N1 | Direct IPv4 to a public IP fails (not via proxy) | guest TCP to `1.1.1.1:53` times out/refused | `scripts/test-network.sh` |
| N2 | Direct IPv6 to a global addr fails (no v4-policy bypass) | guest `AF_INET6` connect to e.g. `2606:4700:4700::1111:53` | `scripts/test-network-ipv6.sh` |
| N3 | ICMP/ping to public net fails (same lock, not SOCKS) | `ping`/`ping6` to public host fails | extend network scripts if needed |
| N4 | Guest proxy env installed without manual `source` | `/var/lib/cage/proxy.env`, `profile.d`, `/etc/environment` (or Windows equivalent) | `test-network.sh` |
| N5 | With proxy env, allowlisted CONNECT succeeds | `curl`/python via `ALL_PROXY` to allowlisted host | optional; egress unit + future smoke |
| N6 | Non-allowlisted CONNECT denied at SOCKS | CONNECT to `example.com` → DENY in log / client error | optional; unit + `vm logs` |
| N7 | `network.proxy.logging` writes JSONL events | `.cage/run/<id>/proxy.log` gains ALLOW/DENY lines | manual / future assert |
| N8 | Softnet/host-only args (or OS equivalent) come from core via `ExtraRunArgs`, not hard-coded egress CIDRs in the plugin | code review + N1–N3 | softnet in core |

## Network — proxy off (`network.proxy.disabled: true`)

| # | Requirement | How to prove | Tart today |
| --- | --- | --- | --- |
| P1 | No host SOCKS / no ExtraRunArgs lock from Cage | status/start without proxy.json; guest can use default net (or backend default) | manual / future |
| P2 | No guest proxy env install | paths from N4 absent | future |

## Isolation boundaries (document, may be separate tests)

| # | Requirement | Notes |
| --- | --- | --- |
| I1 | Guest cannot disable the host-side lock | Softnet/nft/Hyper-V ACL runs on host |
| I2 | Host-only ≠ “SOCKS only” | Guest may still reach other host listeners; keep host attack surface small |
| I3 | Mount/copy are FS trust, not network | Deny list / mount policy is separate from softnet |

## Per-backend matrix

| Backend | GOOS | Host-only mechanism | L* | N1–N4 | N2 (IPv6) | Scripts |
| --- | --- | --- | --- | --- | --- | --- |
| `tart` | darwin | softnet `@host` + block `0.0.0.0/0` | yes | yes | `task test:network:ipv6` | `scripts/test-network*.sh` |
| `incus` | linux | TBD (nftables / nic ACL) | todo | todo | todo | — |
| `hyperv` | windows | TBD (switch ACL) | todo | todo | todo | — |
| `docker` | any | TBD (weaker; document limits) | todo | todo | todo | — |

When adding a backend: implement the contract, then land the same **outcomes** (especially **N1, N2, N4**) under `scripts/` or `go test` with a clear skip if the host lock tool is missing.

## Commands (darwin / Tart)

```bash
task test:integration    # live Tart mount/copy/softnet/bake ITs
task test:network        # proxy env + IPv4 direct blocked
task test:network:ipv6   # IPv6 direct blocked
```
