# Runtime

`runtime.plugins` holds seats for two stages (stage is in the plugin manifest):

| Stage       | Selection                                                                 | Required                                                         |
| ----------- | ------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| **backend** | seats that support this host GOOS, ordered by `priority`, **first match** | yes — missing / no match / no image fails (no invented defaults) |
| **harness** | at most **one** seat (e.g. `pi-agent`)                                    | no — omit = VM-only; copy-in / manual still fine                 |

Harness plugins run on the **host** and supervise the agent process in the guest via backend **Exec**. Seat present → default hooks on `runtime.before_bake`, `runtime.up`, `runtime.down`. Heavy installs attach via **`before_bake`** (hashed derived image) — see [runtime-image-bake](../../plugins/runtime-image-bake.md). Seat YAML `bake:` is optional operator extras. Seat `on-create` stays for **per-VM** leftovers only.

## Fields

| Field     | Meaning                                                                            |
| --------- | ---------------------------------------------------------------------------------- |
| `plugins` | Seat → plugin config (`priority`, `plugin`, `package`, plus stage-specific fields) |
| `workdir` | Working directory inside the guest                                                 |

Override host GOOS with `CAGE_GOOS` for tests.

## Seat fields

| Field        | Meaning                                                                                                                           |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------- |
| `priority`   | Operator-only. Omit if alone at that stage (default `1`). ≥2 seats at the **same** stage → each must set unique explicit priority |
| `plugin`     | Short install name; omit = seat name                                                                                              |
| `package`    | Optional source override (`git:…` or path) when short names collide                                                               |
| `image`      | Backend seats: **base** image (bake may derive a cached image from this + harness inputs)                                         |
| `graphics`   | Backend seats (tart): `true` = show UI; omit/`false` = `--no-graphics`                                                            |
| `goos`       | Backend seats: host GOOS list; omit = plugin default (`tart`→darwin, `incus`→linux, `hyperv`→windows, others→any)                 |
| `on-create`  | Backend seats: host script paths; run once **per VM id** on first `vm start` (not for harness installs — use bake)                |
| `on-start`   | Backend seats: host script paths; run every `vm start` after mounts/copies                                                        |
| `on-destroy` | Backend seats: host script paths; run on `vm delete` (VM must be running)                                                         |
| `bake`       | Backend and/or harness seats: host scripts hashed into a **derived image** (cached until inputs change)                           |

Harness seat fields beyond the reserved keys above are plugin-owned (`version`, `command`, model, args, …). Seats **without** `image` are harness/bake-only (skipped for backend pick). `bake` scripts from the selected backend plus image-less seats feed the derived-image hash; `Ensure` (later) checks them in the guest after Create from the cached image.

Scripts are **per seat** so tart can use `sh` and hyperv `powershell`. Paths are host-relative to the project; the plugin streams them into the guest (no mount required for the script body). They still see mounts/copies under `workdir`.

`on-create` is marked in the guest (`/var/lib/cage/on-create.done` for tart) so re-starts skip it. Profile merge: if a profile sets `on-create` / `on-start` / `on-destroy`, that list **replaces** the base list for that key.

## Plugins

| Plugin | Stage | Default GOOS | Notes |
| --- | --- | --- |
| `tart` | backend | darwin | Micro-VM (v1) |
| `incus` | backend | linux | Later |
| `hyperv` | backend | windows | Later |
| `docker` | backend | any | Later; weaker isolation |
| `pi-agent` | harness | — | Later; host-side supervision of guest process |

## Rollout

- **v1:** `tart` on darwin (mount/copy, seat lifecycle scripts, `Exec`, **bake** derived images)
- **Later:** `incus`, `hyperv`, `docker` bake; harness stage + `pi-agent`

## Shape

```yaml
runtime:
  plugins:
    tart:
      priority: 1
      image: ghcr.io/cirruslabs/ubuntu:latest
      graphics: false
      # on-create: [.cage/scripts/provision.sh]   # per-VM only
      # bake: [.cage/scripts/bake-example.sh]     # derived image (hashed)
      # on-start: [.cage/scripts/boot.sh]
      # on-destroy: [.cage/scripts/cleanup.sh]
    incus:
      priority: 2
      image: ubuntu/24.04
    hyperv:
      priority: 3
      image: Ubuntu
    # docker:
    #   priority: 10
    #   image: ubuntu:24.04
    # pi-agent:            # stage: harness — optional; version feeds bake hash
    #   version: 0.4.2
  workdir: /workspace
```

Named seats when the key ≠ plugin id:

```yaml
runtime:
  plugins:
    darwin-tart:
      priority: 1
      plugin: tart
      image: ghcr.io/cirruslabs/ubuntu:latest
    docker:
      priority: 5
      image: ubuntu:24.04
```

On darwin with both: `darwin-tart` wins (`priority: 1`). Profiles deep-merge per seat.

## Related config

- [`fs.layout`](../fs/layout.md) — how host paths map under `workdir`
- [`fs`](../fs/overview.md) — what is mounted or copied into the guest
- [project structure](../../project-structure.md) — plugin priority rules
- [Runtime backend contract](../../plugins/runtime-backend.md) — `Backend` / `ExtraRunArgs` / network split
- [Derived image bake](../../plugins/runtime-image-bake.md) — hash cache vs `on-create`
