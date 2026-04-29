# Release

Hecate is pre-1.0. Releases should be boring, repeatable, and explicit about
what is alpha-grade versus production-shaped.

## Versioning

- Use semantic-version tags: `v0.x.y` until the public API and storage schema
  reach a v1 stability bar.
- Use patch releases for bug fixes, docs corrections, and small UI polish.
- Use minor releases for API additions, storage changes, provider/runtime
  behavior changes, or operator workflow changes.
- Do not publish a release from a dirty worktree.

## Alpha Gate

Run the full local gate before cutting a public alpha tag:

```bash
make verify-alpha
```

The target runs the non-destructive launch checks in order:

1. docs/env drift check
2. Go unit tests
3. `go vet`
4. Go race tests
5. Docker smoke test
6. UI unit tests
7. UI e2e tests
8. production binary build with embedded UI

If a check is intentionally skipped, call it out in the release notes with the
reason and the risk. Docker smoke and UI e2e are allowed to be slow; they are
not optional for a public alpha build.

## Image Build

Build and smoke-test the local image through the same path used by CI:

```bash
docker compose build hecate
make test-docker-smoke
```

For published images, pin by tag in deployment examples and release notes.
Avoid recommending `latest` for anything beyond quick experiments.

## Release Notes

Each release note should include:

- **Highlights** — the most important operator-visible changes.
- **Breaking or risky changes** — config, storage, API, auth, provider, or UI
  behavior changes that can surprise an operator.
- **Migration notes** — storage/schema considerations and any manual steps.
- **Verification** — the exact gate that passed, normally `make verify-alpha`.
- **Known limitations** — link to [`known-limitations.md`](known-limitations.md)
  and call out any release-specific caveats.

## Alpha Limitations

The public alpha is credible for early technical users, but not a production
SLA. Keep these expectations visible:

- APIs and persisted schemas can still change before v1.
- The gateway/provider path is more mature than the task runtime.
- `sandboxd` is a controlled execution boundary, not hardened OS isolation.
- Multi-node deployments are not the primary tested path yet.
- Custom provider lifecycle is intentionally limited to built-in presets and
  persisted control-plane edits.
