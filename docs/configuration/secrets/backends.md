# Secret backends

`secrets.plugins.<seat>` is a named store seat. Optional `plugin:` is the install name (omit = seat name). Values never appear in config — only refs. Cage resolves them on the host when applying templates.

Prefer `{{ secrets.<seat>.<var> }}` on protocol proxies under `network.plugins`.

## Backends

| `plugin`             | Platform | Use for                                                   |
| -------------------- | -------- | --------------------------------------------------------- |
| `keychain`           | macOS    | Keychain Access items, looked up by name.                 |
| `credential_manager` | Windows  | Windows Credential Manager.                               |
| `secret_service`     | Linux    | FreeDesktop Secret Service (e.g. GNOME Keyring, KWallet). |
| `file`               | any      | Encrypted local file store (path configured on the seat). |
| `onepassword`        | any      | 1Password refs (`op://…`).                                |
| `openai-oauth`       | any      | ChatGPT/Codex subscription OAuth (host-side proxy inject). |
| `aws_sm`             | any      | AWS Secrets Manager (ARN or name + region).               |

OS stores are separate plugins — APIs differ; wrong backend for the host fails clearly.

## Rollout

- **v1:** `onepassword` (`plugins/secrets/onepassword`), `openai-oauth` (`plugins/secrets/openai-oauth`)
- **Later:** `keychain`, `credential_manager`, `secret_service`, `file`, `aws_sm`

## Shape

```yaml
secrets:
  plugins:
    <seat>:
      plugin: onepassword # omit when seat == install name
      package: git:… # optional source override
      uses: [other-seat] # optional explicit deps
      account: … # onepassword: op --account
      app: true # onepassword: desktop CLI integration (omit = true)
      path: … # openai-oauth: auth file (omit = ~/.cage/secrets/openai-oauth/auth.json)
      login: browser # openai-oauth: browser | device_code
      # plugin-specific fields (region, …)
      vars:
        VAR_NAME: <ref> # lookup id; may also embed {{ secrets.<seat>.<var> }}
```

No `priority` on secrets seats. Name-based backends (`keychain`, `credential_manager`, `secret_service`) often use the same string for var name and lookup id.

## Multi-account

Use `account:` on the seat, or a second seat that sets `plugin: onepassword`:

```yaml
secrets:
  plugins:
    onepassword:
      account: my.1password.com
      app: true
      vars:
        OPENAI_API_KEY: op://Engineering/openai/api-key
    organization-op:
      plugin: onepassword
      account: company.1password.com
      vars:
        ANTHROPIC_API_KEY: op://organization/team/docs-agent/anthropic/api-key
```

## Related config

- **`network.plugins.http-proxy` / …** — preferred place for `{{ secrets.<seat>.<var> }}`
- **`runtime.env`** — placeholders preferred; secret templates allowed but put values in the guest
