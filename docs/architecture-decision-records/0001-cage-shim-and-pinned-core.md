# 0001 — Shim on PATH, pinned core in host cache

**Status:** accepted  
**Date:** 2026-09-06

## Context

Users should install one `cage` binary, not Go. Projects still change: YAML, VM IDs, plugins. One host compiler for every repo is the Terraform problem — upgrade for project B, project A breaks.

We need a pin the repo can own, and a user-facing command that does not *be* that compiler.

## Decision

`cage` on PATH is a **shim**. It walks up to the project, reads a **version pin** (not a fat binary in git), fetches that **core** into `~/.cage/versions/` if needed (checksummed), and `exec`s it with the same arguments.

No pin means the shim uses the latest known core (today’s behaviour).  
`.cage/bin/core` may symlink at the cached engine after install. It is not the source of truth and is not committed.

The shim does not load project YAML, start VMs, or serve HTTP. Core does.

A running core is scoped to **one project**. Two repos may run two core versions at once. A host-wide API that compiles every worktree is out of scope here.

## Consequences

- End users: one download (the shim). No Go.
- Repos can pin and stay on an old core while others move.
- We must publish versioned, checksummed cores.
- First run in a pinned project may download. Offline needs a warm cache.
- The shim’s own CLI must stay tiny and stable forever.

## Rejected

- **One immortal `cage` that understands every era** — silent breakage on upgrade.
- **Trust nearest `.cage/bin/core` only** — untrusted clone, huge git, no `cage` outside a repo.
- **Require mise/asdf** — fine as an extra; not how we ship.
- **Shim as a project multiplexer** (`/projects/{id}/*` in the shim) — that’s a later product, not this install model.
