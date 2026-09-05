# Pi agent config (host)

Edit `models.json` and `settings.json` here and commit. Synced to `~/.pi/agent` in the guest on `cage vm start`.

Default path: `.cage/plugins/runtime/pi-agent/`.

ChatGPT/Codex subscription (openai-oauth + http-proxy MITM): use provider **`openai-codex`**, not `openai` with a Codex `baseUrl`. Platform `openai-responses` sends rejected params (`max_output_tokens`, …) → opaque `400 (no body)`. Placeholder JWT `apiKey` is enough for account-id extract; host MITM injects real tokens. Details: [`plugins/secrets/openai-oauth`](../../../../plugins/secrets/openai-oauth/README.md#guest-pi).
