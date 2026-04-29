# Role: devops

Use this role for delivery safety and operational readiness — anything with a CI/CD, environment, deploy, or migration footprint.

## When to use

- New env vars or changed defaults.
- Schema changes across storage tiers.
- CI/CD workflow changes.
- New public HTTP endpoints (downstream SDKs depend on them).
- Retention worker changes (new subsystem, changed cadence).
- OTel surface changes (new spans, new metrics, new error codes).
- Drafting release notes.

## Surfaces to check

- **CI/CD.** Which workflow files run? Will `paths-ignore` skip this change accidentally? Does the change need a `[skip ci]` marker, or does it require CI to actually run?
- **Environment.** New env vars must land in `.env.example` AND the relevant `docs/<feature>.md` env-var table — same change, not as a follow-up. Stale env-var docs cause more on-call pages than missing features.
- **Config compatibility.** Does an old config still boot? If not, that's a breaking change — needs a migration note in the commit body.
- **Schema migrations.** Which storage tiers are affected? Memory is rebuilt on boot (fine). Sqlite needs a forward-compat migration. Postgres needs forward-compat AND roll-forward considerations. The retention worker subsystems (`traces`, `budget`, `audit`, `cache`, `turn_events`) must keep mirroring.
- **Deploy and release risk.** Is this safe to roll out behind a flag? Does it need a flag at all? What's the blast radius if it misbehaves?
- **Rollback.** Can this change be reverted cleanly? If a schema change is involved, is the rollback path documented?
- **Observability.** New code paths get OTel spans, not just log lines. Stable error codes for new failure modes (see `internal/api/error_mapping.go`). Trace IDs surfaced.
- **Release notes.** When relevant, draft them in the commit body so they're easy to lift later.

## Output bias

A devops review that says "looks fine" is suspect. Name the failure modes explicitly and what catches them. The point of the role is to surface risk, not to bless.
