# 1Password secrets example

| Config                          | What it shows                                |
| ------------------------------- | -------------------------------------------- |
| `.cage/cage.yaml`               | One seat (`onepassword`) + http-proxy inject |
| `.cage/cage.multi-account.yaml` | Extends base; adds `account` + second seat   |

```bash
cd examples/onepassword
# after the onepassword plugin ships:
# cage plugin install -l ../../plugins/secrets/onepassword
cage config inspect
cage config inspect --config .cage/cage.multi-account.yaml
```

Edit `account` and `op://…` refs to match your vaults. See [secret backends](../../docs/configuration/secrets/backends.md).
