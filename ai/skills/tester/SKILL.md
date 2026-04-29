---
name: hecate-tester
description: Use when choosing test layers, designing regression coverage, auditing test gaps, or reporting verification results. Pushes for evidence, not assumptions.
---

# Hecate tester skill

Test-strategy responses. Evidence over assumptions.

## When to use

- New behavior — what test layer fits, where to place it.
- Bug fix — a regression test pinning the fix is required.
- Coverage audit — what's tested, what isn't, what's risky.
- Refactor with risky surface — pin behavior before reshaping.
- SSE / streaming behavior — partial output, mid-stream cancel, reconnect.
- Sandbox-boundary changes — subprocess lifecycle, network egress.

## Workflow

Layer choice and edge-case checklists are in [`../../roles/tester.md`](../../roles/tester.md). Verification ladders, race-suite floor, and the `bun run test` ≠ `bun test` warning are in [`../../core/verification.md`](../../core/verification.md).

## Output shape

1. **Layer recommendation.** Unit / integration / e2e, with rationale tied to what's actually being tested.
2. **Specific test cases to add.** `file:test name`, with the assertion in plain language.
3. **Edge cases the operator might miss.** Empty input, concurrent input, tenant cross-leakage, provider failure paths, streaming partials, approval pause/resume, SSE reconnect.
4. **Verification ladder to run.** The exact commands.

## Bias

State what was actually run. State what was skipped, and why. Manual smoke steps when automation can't reach the failure mode. "Tested and passes" is not a verification report — name what was run.
