# Cage web client requirements

## Confirmed

- Run the web client outside the VM.
- Keep Cage and its supervisor inaccessible to the guest agent.
- Send approved prompt and lifecycle operations through the Cage supervisor.
- Receive streamed reasoning, responses, tool activity, status, errors, and completion events.
- Keep durable chat history in the client backend.
- Support multiple chats and recovery of a chat whose Cage session has ended.
- Support interrupt, restart, and stop controls through Cage.
- Use `fs.plugins.mention.suggest` and `fs.plugins.mention.resolve` through the Cage client context.
- The mention plugin owns host indexing, include/exclude policy, host-to-guest translation, and host-only snapshots.
- The web client only renders selected mention records and submits them as prompt context.
- Use authenticated, versioned Cage supervisor and client-context transports.

## Open decisions

- Define supervisor message and event schemas, including request, chat, VM, and harness IDs.
- Define how the web client discovers and reattaches to active Cage sessions.
- Define which lifecycle controls Cage exposes to this client.
- Define authorization per project, VM, and fs plugin seat.
- Define limits for prompts, event frames, snapshots, and concurrent sessions.
- Define chat-store encryption, retention, migration, and interrupted-run recovery.
- Define whether one client can control multiple Cage VMs.
