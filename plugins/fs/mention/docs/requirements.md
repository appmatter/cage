# Mention requirements

## Confirmed

- Run on the Cage host.
- Index paths selected by `fs.plugins.mention.include` and `exclude`; indexing is independent of mounts and copies.
- Support files and directories.
- Return project-relative display paths, kind, and guest visibility.
- Translate a host path to its guest path when it is under an `fs.mount` or `fs.copy` source.
- Use `fs.layout`: `flat` maps targets under `runtime.workdir`; `host` maps targets below `/`.
- A selected file outside the guest filesystem is sent as a bounded content snapshot.
- A selected directory outside the guest filesystem is sent as a bounded recursive listing. File contents require explicit file selections.
- Do not expose absolute host paths to clients.
- Apply include/exclude rules to suggestions and resolution.

## Limits

- Cap indexed results, suggestions, snapshot bytes, directory entries, and recursion depth.
- Reject paths outside configured index roots and paths that do not match policy.
- Re-check the path and policy when resolving a selected mention.

## Cage interface

Cage configures the plugin with host index roots and resolved mount/copy mappings. The plugin exposes:

- `Suggest(query, limit)` for file and directory suggestions.
- `Resolve(paths)` for validated mention snapshots.

Cage exposes these operations to client backends. Browsers do not call the plugin or receive host paths.

## Open decisions

- Define index source modes (`explicit`, `fs`, `git`) and their config shape. Initial behavior is explicit project indexing.
- Decide whether `fs.deny` and `secrets_scanner` findings add an implicit mention exclusion.
- Define the supervisor API and the prompt-context wire schema.
