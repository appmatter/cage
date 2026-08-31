# Secret backends

`secrets.<plugin>.<alias>` is a named store. The parent key is the backend plugin; each alias maps variable names to references. Values never appear in config — only refs. Cage resolves them on the host when applying templates.

Prefer `{{ secrets.<alias>.<var> }}` on protocol proxies under `network.plugins` (keep aliases unique across plugins).

## Backends

| `plugin`             | Platform | Use for                                                    |
| -------------------- | -------- | ---------------------------------------------------------- |
| `keychain`           | macOS    | Keychain Access items, looked up by name.                  |
| `credential_manager` | Windows  | Windows Credential Manager.                                |
| `secret_service`     | Linux    | FreeDesktop Secret Service (e.g. GNOME Keyring, KWallet).  |
| `file`               | any      | Encrypted local file store (path configured on the alias). |
| `onepassword`        | any      | 1Password refs (`op://…`).                                 |
| `aws_sm`             | any      | AWS Secrets Manager (ARN or name + region).                |

OS stores are separate plugins — APIs differ; wrong backend for the host fails clearly.

## Rollout

- **v1:** `onepassword`
- **Later:** `keychain`, `credential_manager`, `secret_service`, `file`, `aws_sm`

## Shape

```yaml
secrets:
  <plugin>: # onepassword | keychain | credential_manager | secret_service | file | aws_sm
    <alias>:
      uses: [other-alias] # optional explicit deps
      # plugin-specific fields (vault, region, path, …)
      vars:
        VAR_NAME: <ref> # lookup id; may also embed {{ secrets.<alias>.<var> }}
```

No `priority` on secrets plugins or aliases. Name-based backends (`keychain`, `credential_manager`, `secret_service`) often use the same string for var name and lookup id.

## Related config

- **`network.plugins.http-proxy` / …** — preferred place for `{{ secrets.<alias>.<var> }}`
- **`runtime.env`** — placeholders preferred; secret templates allowed but put values in the guest

## Examples

```yaml
secrets:
  onepassword:
    personal-op:
      vars:
        OPENAI_API_KEY: op://Engineering/openai/api-key

runtime:
  env:
    OPENAI_API_KEY: "fake-key-for-agent"

network:
  plugins:
    http-proxy:
      openai:
        url: https://api.openai.com/v1
        headers:
          Authorization: "Bearer {{ secrets.personal-op.OPENAI_API_KEY }}"
```
