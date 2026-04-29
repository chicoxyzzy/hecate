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

## Snapshot Dry-Run

Before pushing the tag, run goreleaser locally in snapshot mode to catch
release-config issues without publishing anything:

```bash
goreleaser release --snapshot --clean
```

This builds the same set of artifacts the CI release workflow does — Go
binaries for `linux+darwin × amd64+arm64` and the per-arch Docker images —
into `./dist`, but skips publishing to GHCR and skips creating a GitHub
release. The run takes ~2-3 minutes and surfaces almost every config issue
you'd otherwise hit on the real tag push.

**Inspect the auto-generated changelog.** The first tag in the repo lists
every commit since the dawn of git history; subsequent tags list only commits
since the previous tag. If the changelog is unusable, tune
`.goreleaser.yaml`'s `changelog.filters` or use `--release-notes <file>` to
override before tagging.

**Pre-flight checks before the snapshot run:**

- `git status` is clean. Goreleaser refuses to release from a dirty worktree
  (and the snapshot run is the rehearsal — same constraint applies).
- `dist/` is gitignored at repo root. The snapshot writes binaries and
  tarballs into `./dist`; if the directory is tracked, those artifacts can
  leak into a follow-up commit and break the next release on `--clean`. The
  `ui/dist/` entry in `.gitignore` does **not** cover repo-root `dist/`.
- `goreleaser` itself is on PATH (`go install github.com/goreleaser/goreleaser/v2@latest`).

## Tag and Push

After the snapshot dry-run passes:

```bash
git tag -a v0.x.y -m "..."     # annotated tag with release notes
git push origin v0.x.y         # triggers .github/workflows/release.yml
```

For pre-release tags, use the dotted-suffix semver form:
`v0.1.0-alpha.1`, `v0.1.0-alpha.2`, `v0.1.0-rc.1`. Goreleaser handles them
the same way; consumers can opt out via semver tooling that recognizes
pre-release tags.

If the published CI run fails, recover with:

```bash
git push --delete origin v0.x.y
git tag -d v0.x.y
# fix root cause, retag, retry
```

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
