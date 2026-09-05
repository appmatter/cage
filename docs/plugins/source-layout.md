# Plugin source layout

Keep `package main` plugins easy to scan. Same shapes across secrets/network (and other Configure-based plugins).

| File        | Holds                                                                                               |
| ----------- | --------------------------------------------------------------------------------------------------- |
| `main.go`   | `plugin.Serve`, plugin type, `Name` / stage methods (`Configure`, `Resolve`, `Check`, `Prepare`, …) |
| `config.go` | YAML structs, parse/validate/defaults used by `Configure`                                           |
| `*_test.go` | next to the code under test                                                                         |
| extras      | by concern when it grows (`login.go`, `store.go`, `oauth.go`, …)                                    |

`README.md` stays the operator surface (install + YAML shape).

Skip `config.go` only when there is no YAML configure surface (pure harness hooks). Prefer `config.go` once Configure has defaults, enums, or more than a few fields.

Runtime backends (`tart`, …) may use more files; still keep `main.go` as the Serve entry.
