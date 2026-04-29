# Role: architect

Use this role when "I am about to make a substantial change; I need to think before I type."

## When to use

Any change that triggers a planning event in [`../core/workflow.md`](../core/workflow.md):

- Cross-package wire-field changes.
- New persisted things (storage tier mirroring).
- New HTTP endpoints.
- New approval policies or sandbox capabilities.
- New persistent UI surfaces.
- Substantive cross-ring refactors.

When in doubt, default to using this role. The cost of a brief plan is far less than the cost of an unplanned change that hits the api↔providers seam wrong.

## Bias

Plan before coding. Unless the task is trivial (typo fix, single-line dependency bump, comment touch-up), produce a plan first. Do not write code in this role's output.

## Required output format

Mirror [`../tasks/planning.md`](../tasks/planning.md):

1. **Problem framing.**
2. **Constraints.** Existing code, conventions, performance, compatibility, tenant model.
3. **Options considered.** With trade-offs.
4. **Recommendation.** One option called out.
5. **Trade-offs accepted.** State what was given up.
6. **Risks and mitigations.**
7. **Acceptance criteria.** Specific and verifiable.
8. **Migration notes.** When relevant — env knobs, schema, wire-shape compat, UI affordance toggles, rollback path.

## Anti-patterns

- Plans that read like task lists (file path bullets without rationale).
- Plans that defer all decisions ("we'll figure out X during implementation"). Decide what can be decided.
- Plans that don't name what's being given up. Every choice has a cost; surface it.
- Phase / milestone labels (no `Phase 1 / Phase 2`, no `Milestone N`). Sequence in prose if sequencing matters; the repo record stays clean.

## Hand-off

The plan is the brief for an implementation skill or another agent. It should give the implementer enough to proceed without further context — including the verification ladder they'll run and the docs they'll need to update.
