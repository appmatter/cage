# Project structure

Go module at the repo root. Host-side Cage CLI and supervisor.

**Core owns contexts** (platforms with pipelines and hook points). **Plugins attach to a context/stage** and may own a config key. Config YAML mirrors that tree — flat invent-your-own top-level keys are not the model.

```
.
├── cmd/cage/main.go                 # thin CLI + plugin host
├── go.mod
├── internal/
│   ├── cli/
│   ├── config/                       # load/merge; schema follows contexts
│   ├── pluginhost/
│   ├── host/                         # host context (machine Cage runs on)
│   ├── secrets/                      # secrets context (resolve on host)
│   ├── runtime/                      # runtime context
│   ├── fs/                           # fs context (mount/copy/deny + plugins)
│   └── network/                      # network.traffic pipeline
├── pkg/plugin/v1/                    # contracts per context/stage
│   ├── runtime/
│   ├── secrets/
│   ├── fs/                           # e.g. mention, secrets_scanner
│   └── network/                      # traffic stages (filter, terminate, …)
├── plugins/                          # each plugin ships its own README
│   ├── runtime/tart/
│   ├── runtime/pi-agent/             # later — harness stage
│   ├── secrets/onepassword/
│   ├── fs/mention/
│   ├── fs/secrets_scanner/
│   └── network/
│       ├── egress/
│       ├── http-proxy/
│       └── postgres-proxy/
├── docs/                             # core only; link to plugin READMEs
│   ├── project-structure.md
│   └── configuration/…
└── .cage/
```

## Contexts (core, fixed)

| Context   | Job                                                              | Hook points (sketch)                           |
| --------- | ---------------------------------------------------------------- | ---------------------------------------------- |
| `host`    | Machine Cage runs on                                            | `host.*`                                       |
| `runtime` | Guest VM/process lifecycle, workdir, env                         | `runtime.before_bake`, `runtime.on_start`, `runtime.on_attach_shell`, `runtime.up`, `runtime.down`, … |
| `fs`      | What the guest can see; fs plugins (mention, secrets_scanner, …) | `fs.before_apply`, `fs.after_apply`            |
| `secrets` | Named credential stores (resolved on host)                       | `secrets.*`                                    |
| `network` | Outbound path                                                    | see `network.traffic`                          |

Hook points are fixed on the context. At **plugin build time**, a plugin declares which hooks it attaches to (manifest / contract). Seat present under `runtime.plugins` → core calls it at those events. Operator `runtime.hooks.<event>` only for non-default extra wiring. Same pattern as bake: plugin returns contributions (scripts, env, …); it does not edit YAML.

Seat YAML `on-create` / `on-start` / `on-destroy` remain **operator script lists** on the backend seat (plugin runs them). Plugin-declared hooks are separate — e.g. harness attaches `before_bake` + `on_start` + `on_attach_shell` in its binary, not by writing those keys into config.

### `runtime` stages

Same `runtime.plugins` map; stage is in the plugin manifest (not a nested config tree).

1. **backend** — VM/container lifecycle (`tart`, `incus`, …). Among seats that support this host GOOS, **first match wins**.
2. **harness** — supervise the agent process **inside** the guest (`pi-agent`, …). Host-side plugin; uses backend **Exec**. **0 or 1** active seat (none = VM-only / copy-in). Presence enables default lifecycle on `runtime.up` / `runtime.down` and **`runtime.before_bake`** (bake attachments).

**Derived image bake** (before Create): core runs `runtime.before_bake` hooks; plugins attach scripts/steps that are hashed into a derived image. Seat YAML `bake:` is an optional operator extra (same bucket). Not the same as seat `on-create` (once per VM id). See [runtime-image-bake](./plugins/runtime-image-bake.md).

Order on up: **`before_bake`** → bake/cache resolve → backend Start (mounts/copies → seat `on-create` once → `on-start`) → env inject → harness Ensure/Start. On down: harness Stop → seat `on-destroy` (if set; VM must be running) → backend Stop/Delete. Core owns no harness-specific knowledge — only the contract plus bake hashing/cache. Seat lifecycle scripts are **plugin-owned** (bash vs powershell); heavy installs belong in bake via `before_bake`.

Backend plugin contract (GOOS-agnostic, including `ExtraRunArgs` / host proxy): [runtime-backend](./plugins/runtime-backend.md). Conformance tests per backend: [runtime-backend-conformance](./plugins/runtime-backend-conformance.md).

### `network.traffic` pipeline

Ordered stages (not a bag of plugins). Missing required stage → fail closed.

1. **filter** — allow/deny **upstream destination** (egress). Proxies do not skip this; proxied hosts must be allowlisted.
2. **terminate** — optional protocol proxy (secrets inject on host)
3. **dial** — core opens the upstream

Agent → local proxy listen is host plumbing. Egress answers “may we reach that outside host?”

### Plugin priority (operator config only)

When multiple plugins share the same **context + stage**, Cage orders them by `priority` (ascending, lower first).

| Rule        | Detail                                                                     |
| ----------- | -------------------------------------------------------------------------- |
| Who sets it | **Operator config only.** Plugin manifests must not declare priority.      |
| Default     | `1` when that stage has a **single** plugin (omit `priority`)              |
| Two or more | Each must set `priority` **explicitly**; values must be **unique**         |
| Not a list  | No ordered `traffic: [a, b]` list — priority merges cleanly with `extends` |
| `secrets`   | **Exempt.** No plugin priority. Resolve by dependency DAG (see below).     |

```yaml
network:
  plugins:
    egress: # alone at filter → priority defaults to 1
      allow: […]
    http-proxy:
      priority: 1 # terminate: two plugins → both explicit
      openai: { … }
    postgres-proxy:
      priority: 2
      db: { … }
```

Filter composition: run ascending priority; **all must allow** (any deny wins). Terminate: each plugin owns its protocol; priority orders setup only. Runtime **backend**: among seats that support this host GOOS, **first match wins**. Runtime **harness**: at most one seat; alone → omit priority (default `1`).

### Secrets ordering

Map / document order does not matter. Seat keys under `secrets.plugins` are unique. Stores may declare `uses: [other-seat]` and/or `{{ secrets.<seat>.<var> }}` in their own config; Cage topo-resolves and **fails on cycles**. Host reachability (`aws.sso_profile`) is separate from secret deps.

## Config mirrors contexts

Core keys and installable seats are separated: plugins live under `<context>.plugins`.

```yaml
version: 1

runtime:
  plugins:
    tart:
      priority: 1
      image: ghcr.io/cirruslabs/ubuntu:latest
      # on-create / on-start / on-destroy — host script paths; plugin runs them
    docker:
      priority: 5
      image: ubuntu:24.04
    pi-agent: # stage: harness (manifest); optional
      # command / model / args — plugin-owned fields
  workdir: /workspace
  env: { … }

fs:
  layout: { mode: flat }
  mount: { … }
  copy: { … }
  deny: […]
  plugins:
    mention:
      include: […]
      exclude: […]
    secrets_scanner:
      on_find: warn
      allow: [OPENAI_API_KEY]

secrets:
  plugins:
    personal-op:
      plugin: onepassword
      vars: { … }

network:
  plugins:
    egress:
      allow:
        - host: www.package-readme.com
          port: 443
        - host: api.openai.com
          port: 443
          method: POST
          path: /v1/chat/completions
    http-proxy:
      priority: 1
      openai:
        url: https://api.openai.com/v1
        headers:
          Authorization: "Bearer {{ secrets.personal-op.OPENAI_API_KEY }}"
    postgres-proxy:
      priority: 2
      db:
        listen: 5432
        host: db.example.com
```

| Config path                             | Context                       | Owner                                                                 |
| --------------------------------------- | ----------------------------- | --------------------------------------------------------------------- |
| `runtime.plugins.<seat>`                | `runtime` / backend           | runtime backend plugins (`plugin` + `image`; pick by GOOS + priority) |
| `runtime.plugins.<seat>`                | `runtime` / harness           | harness plugins (e.g. `pi-agent`; 0 or 1; needs backend Exec)         |
| `runtime.workdir` / `env` / `hooks`     | `runtime`                     | core                                                                  |
| `fs.mount` / `copy` / `deny` / `layout` | `fs`                          | core                                                                  |
| `fs.plugins.mention`                    | `fs`                          | `mention` plugin                                                      |
| `fs.plugins.secrets_scanner`            | `fs`                          | `secrets_scanner` plugin                                              |
| `secrets.plugins.<seat>`                | `secrets`                     | secrets plugins (DAG, no priority)                                    |
| `network.plugins.egress`                | `network.traffic` → filter    | `egress` plugin                                                       |
| `network.plugins.http-proxy`            | `network.traffic` → terminate | `http-proxy` plugin                                                   |
| `network.plugins.postgres-proxy`        | `network.traffic` → terminate | `postgres-proxy` plugin                                               |

Secret templates: `{{ secrets.<seat>.<var> }}` — prefer on protocol proxies; allowed in `runtime.env` but weaker.

## Plugins

Independently installable binaries (go-plugin). Core stays thin; plugins ship and version on their own.

```bash
cage plugin install git:github.com/acme/cage-runtime-tart@abc1234
cage plugin install ./plugins/runtime/tart
cage plugin list
cage plugin remove runtime/tart
```

- **Global:** `~/.cage/.cache/plugins/`
- **Project binaries:** `.cage/.cache/plugins/` (gitignored) + `.cage/plugins.lock.json`
- **Project artefacts:** `.cage/plugins/<kind>/<name>/` (committed; e.g. pi `models.json`)
- **Pin:** git commit or tag

Manifest sketch (**no `priority`** — operator sets that in config):

```json
{
  "name": "tart",
  "context": "runtime",
  "stage": "backend",
  "config": "runtime.plugins",
  "command": "cage-runtime-tart.cageplugin",
  "source": "git:github.com/acme/cage-runtime-tart",
  "pin": "abc1234def..."
}
```

### Plugin identity and `package`

| Layer          | Unique by                                                                                    |
| -------------- | -------------------------------------------------------------------------------------------- |
| Lock / install | `source` (+ pin). Short `name` is the local install id                                       |
| Config seat    | Key under `*.plugins` (alias). Optional `plugin:` = short name; optional `package:` = source |

**Install collision:** two packages both shipping manifest `name: egress` → second install **fails** if it would reuse `network/egress` from a different source. Fix: `cage plugin install -l git:…/acme-egress --name acme-egress`, then:

```yaml
network:
  plugins:
    egress:
      allow: […]
    acme-egress:
      priority: 10
      package: git:github.com/acme/cage-egress
      allow: […]
```

`package` is optional when the seat key / `plugin:` already matches a unique lock `name`. Never last-install-wins. Plugin manifests must not set priority (or override another package’s seat silently).

| Plugin                                 | Context / stage               | Config                           |
| -------------------------------------- | ----------------------------- | -------------------------------- |
| `tart` / `incus` / `hyperv` / `docker` | `runtime` / backend           | `runtime.plugins.<seat>`         |
| `pi-agent` / …                         | `runtime` / harness           | `runtime.plugins.<seat>`         |
| `egress`                               | `network.traffic` / filter    | `network.plugins.egress`         |
| `http-proxy` / `postgres-proxy` / …    | `network.traffic` / terminate | `network.plugins.<plugin>`       |
| `onepassword` / …                      | `secrets`                     | `secrets.plugins.<seat>` (no priority) |
| `mention`                              | `fs`                          | `fs.plugins.mention`             |
| `secrets_scanner`                      | `fs`                          | `fs.plugins.secrets_scanner`     |

Unknown plugin ref → fail. Core does **not** invent backends/images/secrets stores.

## Layout notes

- First-party plugins under `plugins/` but install like any other source
- Prefer git commit pins for teams; tags for releases
- Fail closed: no egress allow → no outbound
