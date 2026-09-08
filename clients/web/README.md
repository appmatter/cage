# Cage web client

Go + HTMX web client. Its backend holds the Cage supervisor WebSocket; the browser uses forms and SSE.

It stores chats locally. Cage owns live VM and harness sessions. Reopening a cold chat hydrates its saved user and assistant turns.

Mentions use Cage's configured `fs.plugins.mention` capability. The web client does not index or read workspace files.

## Config

| Field | Meaning |
| --- | --- |
| `listen` | UI bind address |
| `supervisor` | Cage supervisor WebSocket URL |
| `cage` | Cage client-context API URL |
| `token` | Cage bearer token, if required |
| `data_dir` | Local durable chat store |

Type `@` to search files and directories offered by Cage. Selected entries are resolved by Cage before prompt submission.

## Prerequisites

- Go 1.24 or newer.
- A running Cage supervisor with its client-context API enabled.
- `fs.plugins.mention` configured in the active Cage config.
- The mention plugin installed for that Cage project:

```bash
cage plugin install -l ./plugins/fs/mention
```

## Build and run

Create `config.json` from `config.example.json` and set the Cage URLs and token.

```bash
cd clients/web
go build -o cage-web .
./cage-web -config config.json
```

For development, install [Air](https://air-verse.github.io/air/) and run:

```bash
air -- -config config.json
```

Open `http://127.0.0.1:3000`. Flags `-supervisor`, `-listen`, `-cage`, and `-token` override config values.
