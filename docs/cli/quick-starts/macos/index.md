# CLI quick start (macOS)

Install the host CLI on your PATH, init a project, install plugins, create and run a VM. These instructions are only for macOS.

## Prerequisites

- Go 1.22+
- [Tart](https://tart.run) on PATH — setup: [tart](../../../plugins/runtime/tart/README.md)
- [softnet](https://github.com/cirruslabs/softnet) on PATH when host proxy is on (default). Setup: [softnet](../../../plugins/runtime/tart/docs/softnet.md). Skip with `network.proxy.disabled: true`.
- `$(go env GOPATH)/bin` on your PATH (usually `~/go/bin`)

## Install CLI

From this repo:

```bash
go install ./cmd/cage
```

That puts `cage` on your PATH (via `GOPATH/bin`). Confirm with `which cage` / `cage --help`.

### Tab completion

```bash
go install ./cmd/cage
cage completion install   # writes e.g. ~/.zsh/completions/_cage
exec zsh                   # or the reload hint printed by install
```

Put that completions dir on `fpath` in `~/.zshrc` (once) if it is not already. `cage completion generate zsh` prints the script to stdout if you want to place it yourself.

Local build without installing:

```bash
go build -o bin/cage ./cmd/cage
```

## Init a project

```bash
cage init
```

Writes `.cage/cage.yaml`, `.cage/plugins.lock.json`, `.cage/run/`, and `.cage/.gitignore` (ignores `.cache/` and `run/`). Existing files are left alone unless `--force`.

## Plugins (team share)

Binaries are not committed. The lock file is.

```bash
# install and pin into .cage/plugins.lock.json
cage plugin install -l ./plugins/runtime/tart
cage plugin install -l ./plugins/network/egress
# or: cage plugin install -l git:github.com/org/cage-runtime-tart@abc1234

# short name collision (two packages both named egress) — install under an alias:
cage plugin install -l git:github.com/acme/cage-egress --name acme-egress
# then in config: package: git:github.com/acme/cage-egress on that seat

# after clone — install binaries from the lock
cage plugin install

cage plugin list
cage plugin init                    # interactive — see CLI menus
cage plugin init runtime/pi-agent   # seed .cage/plugins/runtime/pi-agent/
```

`-l` = project (`.cage/.cache/plugins` + lock). Without `-l` = user global (`~/.cage/.cache/plugins`, no lock). Same `kind/name` from a **different** source fails (no silent replace).

## Inspect (before create)

Shows resolved backend/image and every mount, copy, and deny after profile merge.

```bash
cage config inspect
cage config inspect --config .cage/cage.docs-agent.yaml
```

## VM lifecycle

No `--config`: one `cage*.yaml` → use it; several → interactive select ([CLI menus](../../conventions/menus.md)).

`cage vm start` applies resolved **mounts** (`tart run --dir`) and **copies** (into the guest after the VM is ready). Paths on `fs.deny` fail at `LoadResolved` / inspect when used as mount/copy hosts; matching descendants under an allowed mount are masked in the guest at start.

By default start also runs a host **HTTP CONNECT** proxy (MITM on) plus SOCKS after the VM is up, allowlisting that guest’s source IP so peers cannot use each other’s proxy. Softnet host-only blocks direct internet ([softnet setup](../../../plugins/runtime/tart/docs/softnet.md)). Allowlists live on `network.plugins.egress`. Opt out with `network.proxy.disabled: true`. Set `network.proxy.logging: true` for CONNECT JSONL at `.cage/run/<id>/proxy.log` — follow with `cage vm logs -f` or `cage vm start -f`. Install egress when using an allowlist: `cage plugin install -l ./plugins/network/egress`. Guest proxy env uses `http://` (Node-safe): `/var/lib/cage/proxy.env`, profile hooks, `/var/lib/cage/shell`, and MITM CA install. Agents call real `https://…` URLs; Cage injects via `http-proxy` Host match. Certificate pinning may still break some clients.

```bash
cage vm create
cage vm start
cage vm exec          # interactive login shell
cage vm exec -- uname -a
cage vm status
cage vm stop
cage vm delete

# or explicit
cage vm status --config .cage/cage.yaml
```

Default VM name is `cage-vm` (override with `--id` or `CAGE_VM_ID`).

## Tart / network integration tests

Live Tart/runtime ITs (`//go:build integration && darwin`) — not in `go test ./...`:

```bash
task test:integration
# or: go test -tags integration ./plugins/runtime/...
```

- `TestIntegrationMountAndCopy` — mount + copy
- `TestIntegrationSoftnetHostOnly` — softnet host-only (skips if softnet/privileges missing)
- `TestIntegrationRuntimeEnvSecrets` — resolve `{{ secrets.* }}` → guest `runtime.env`
- `TestIntegrationBeforeBakeOnTart` — pi-agent BeforeBake on Tart

Full proxy path smoke (rebuild CLI+plugins, headless VM, timeouts on guest exec via `cage vm exec`):

```bash
task test:network
task test:network:ipv6   # softnet must not allow IPv6 direct egress
# or: go test -tags network ./internal/network/
```

Skips (exit 0) when Tart, softnet, root setuid, or local image is missing. Default image `ubuntu`; override with `CAGE_TART_IMAGE`. See [conformance](../../plugins/runtime-backend-conformance.md).

## Useful commands

```bash
cage init --force
cage plugin remove -l runtime/tart
cage vm --help
cage plugin --help
```

## Related

- [CLI conventions](../../conventions/index.md) — command shape, [menus](../../conventions/menus.md), [color](../../conventions/color.md)
- [Derived image bake](../../plugins/runtime-image-bake.md)
