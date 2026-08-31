# Tart (macOS runtime)

[Tart](https://tart.run) is the darwin **backend** for Cage: create/start/stop Linux VMs via Apple Virtualization. Softnet (host-only networking) is separate — see [softnet.md](./docs/softnet.md).

## Install

```bash
brew install cirruslabs/cli/tart
tart --version
```

Must be on `PATH` for `cage vm *` when the resolved backend is `tart`.

## Images

Cage’s seat `runtime.plugins.tart.image` is a Tart VM name or OCI ref. Pull/clone once:

```bash
# common local name used in fixtures / docs
tart clone ghcr.io/cirruslabs/ubuntu:latest ubuntu

# or use the OCI ref directly in cage.yaml
# image: ghcr.io/cirruslabs/ubuntu:latest
```

Override smoke-test image with `CAGE_TART_IMAGE` (default `ubuntu`).

Cirrus Ubuntu images already include the **Tart Guest Agent** (needed for `tart exec` / `cage vm exec`). Vanilla images without the agent will fail exec.

## Cage plugin

```bash
cage plugin install -l ./plugins/runtime/tart
```

Binary lands as `.cage/.cache/plugins/runtime/tart/cage-runtime-tart.cageplugin`.

## Notes

- `graphics: true` opens a Tart window after headless setup; headless CI/smokes use `graphics: false`.
- `tart exec` has **no** `--` separator — use `tart exec <vm> <command>…` (or `cage vm exec`).
- Interactive `cage vm exec` runs `tart exec -it` on the host terminal (not via go-plugin pipes).
- Guest agent / control socket issues are Tart-side; see [upstream Tart](https://github.com/cirruslabs/tart).
