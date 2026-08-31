# `fs` overview

`fs` is the filesystem context: core mount/copy/deny/layout, plus installable plugins under `plugins`.

| Doc | Key | Meaning |
| --- | --- | --- |
| [layout](./layout.md) | `layout` | Guest path placement (`flat` \| `host`) — **core** |
| [mount](./mount.md) | `mount` | Live bind to the host — **core** |
| [copy](./copy.md) | `copy` | One-shot seed into the guest — **core** |
| [deny](./deny.md) | `deny` | Block matching mount/copy host paths — **core** |
| [mention](../../../plugins/fs/mention/README.md) | `plugins.mention` | Host @mentions / search |
| [secrets_scanner](../../../plugins/fs/secrets_scanner/README.md) | `plugins.secrets_scanner` | Flag likely secrets in guest-visible surfaces |

```yaml
fs:
  layout: { mode: flat }
  mount: { … }
  plugins:
    mention: { … }
    secrets_scanner: { … }
```

See [secrets_scanner](../../../plugins/fs/secrets_scanner/README.md) and [project structure](../../project-structure.md).
