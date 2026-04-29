---
name: hecate-architect
description: Use when planning a substantial change before coding — new wire fields, new persisted things, new endpoints, new persistent UI surfaces, cross-package refactors. Produces a structured plan, not code.
---

# Hecate architect skill

Plan-shaped responses for substantial changes. The skill produces a plan, not code.

## When to use

- Cross-package wire-field changes (the seven-step chain — see [`../providers/SKILL.md`](../providers/SKILL.md)).
- New persisted things — must mirror memory + sqlite + postgres tiers.
- New HTTP endpoints (public `/v1/...` or admin `/admin/...`).
- New approval policies or sandbox capabilities.
- New persistent UI surfaces (inspector, side rail, dashboard block, summary panel).
- Substantive refactors that cross ring boundaries or touch the api↔providers seam.

## Bias

Plan first. Do not write code in this skill's response. The cost of a plan is far less than the cost of an unplanned change that hits the wrong seam.

## Required output shape

Use the plan template from [`../../roles/architect.md`](../../roles/architect.md). Format expectations are in [`../../tasks/planning.md`](../../tasks/planning.md). This skill is a thin pointer — the substance lives in those two files.

## Hand-off

The plan is the brief for a `hecate-backend`, `hecate-ui`, or `hecate-providers` skill turn. It should give the implementer everything they need to proceed without further context — including the verification ladder they'll run and the docs they'll update.

## Verification expectations

None for the plan itself. The plan must enumerate the verification ladder for the implementer.
