---
name: hecate-devops
description: Use when reviewing delivery safety — env-var changes, schema migrations, deploy risk, rollback paths, observability surfaces, release notes.
---

# Hecate devops skill

Delivery-readiness review. Surfaces risk explicitly.

## When to use

- New env vars or changed defaults.
- Schema changes across storage tiers (memory + sqlite + postgres).
- CI/CD workflow changes.
- New public HTTP endpoints (downstream SDKs depend on them).
- Retention worker changes (new subsystem, changed cadence, retention windows).
- OTel surface changes (new spans, new metrics, new error codes).
- Release-note drafting.

## Workflow

The full surface checklist is in [`../../roles/devops.md`](../../roles/devops.md). This skill is a thin pointer — the substance lives there.

## Output shape

1. **Surfaces affected.** Env, schema, CI, observability — name each one explicitly.
2. **Rollout risk.** Blast radius if this misbehaves; blocking vs non-blocking; flag-gateable or not.
3. **Rollback path.** Can this revert cleanly? If a schema change is involved, is the rollback documented?
4. **Doc updates required.** `.env.example`, `docs/<feature>.md`, `docs/events.md`, `docs/runtime-api.md` — whichever apply.
5. **Draft release-note line.** When relevant.

## Bias

Surface failure modes explicitly. A devops review that says "looks fine" is suspect. Name the failure mode and what catches it; if nothing catches it, that's the finding.
