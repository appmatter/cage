# Working rules

## Communication

Use short, plain English. Lead with the result. Avoid filler and excessive formatting.

## Scope

Inspect the relevant implementation before editing.

Change only what the request requires. Do not implement later roadmap items, speculative functionality or unrelated cleanup.

Preserve existing behaviour unless the request explicitly changes it.

If a material requirement is unclear, stop and ask rather than inventing scope.

One concept per PR or commit. A reviewer should grasp the change from the title and a short diff.

If a request covers more than one concept, or will touch many unrelated files, stop. Propose a numbered breakdown (one concept each) and wait. Do not start the large change.

## Code

Prefer the smallest change that satisfies the request.

Do not add abstractions, wrappers, compatibility layers or exported APIs unless the current change needs them.

When extracting a boundary, first delegate to existing behaviour. Move implementation and change behaviour only in separately requested steps.

Doc comments: one short sentence, only when the name and signature do not state the contract. Do not restate the function name, path or obvious behaviour. No inline comments except for a non-obvious invariant.

## Tests

Add tests for behaviour, regressions or meaningful contracts.

Do not test trivial delegation, type aliases or constants.

Run focused tests for changed behaviour. Run broader tests only when justified.

## Files and documentation

Do not modify unrelated files or reformat untouched code.

Keep Markdown short, professional and easy to scan. Prefer focused documents linked together over large guides.

## Commands

Normal local build, test, lint and formatting commands are allowed.

Do not run GitHub, AWS, Pulumi, CDK or other cloud and infrastructure commands.

Read-only git inspection is allowed (`git status`, `git log`, `git diff`, `git show`). Do not mutate git state unless the user asks.

`terraform fmt` is allowed. Do not run other Terraform commands.
