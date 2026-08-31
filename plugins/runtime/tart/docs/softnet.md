# Softnet (macOS host proxy)

Softnet sits on the Tart VM network path so the guest can only reach the **host** (and thus Cage’s SOCKS proxy). It needs **root once at startup** to create Apple’s vmnet interface and tweak DHCP; it then drops privileges.

Required when `network.proxy.disabled` is false/omitted (default). Skip softnet entirely with `network.proxy.disabled: true` (open guest networking; debug only).

## Install + privileges

```bash
brew trust --formula cirruslabs/cli/softnet
brew install cirruslabs/cli/softnet

# Homebrew does not set this for you — required or tart run fails with
# "root privileges are required" / "Softnet process terminated prematurely":
sudo chown root "$(brew --prefix softnet)/bin/softnet"
sudo chmod u+s "$(brew --prefix softnet)/bin/softnet"
ls -la "$(brew --prefix softnet)/bin/softnet"   # expect root owner and s: -r-sr-xr-x root
```

`u+s` = setuid: when Tart runs softnet, the process starts as the **file owner**. Owner must be **root** or you only get your own (useless) privileges. Alternative: allow passwordless sudo **only for softnet** (e.g. a sudoers line for that binary) — not passwordless sudo for everything.

Re-run `chown`/`chmod` after `brew reinstall softnet` / upgrades (ownership and setuid do not stick across reinstalls).

## What Cage passes

With the host proxy on, core sets Tart `ExtraRunArgs` to softnet host-only (block all IPv4, allow `@host`). Egress allowlists run on the SOCKS proxy, not as softnet CIDR lists. See [runtime backend](../../../../docs/plugins/runtime-backend.md) and [egress](../../../network/egress/README.md).

## Logging

Softnet drops packets **before** they hit SOCKS, so there is no per-packet drop stream from softnet into Cage. With `network.proxy.logging: true`, `proxy.log` / `cage vm logs -f` still gets:

- a `SOFTNET` line when the proxy starts (host-only is active; direct internet outside SOCKS is dropped silently by softnet)
- a one-shot guest probe on `vm start` that tries direct TCP to `1.1.1.1:53` and logs whether that was blocked (expected)

SOCKS allow/deny/fail remain separate CONNECT events.